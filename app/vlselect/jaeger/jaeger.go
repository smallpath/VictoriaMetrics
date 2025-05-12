package jaeger

import (
	"context"
	"fmt"
	"github.com/VictoriaMetrics/VictoriaMetrics/app/vlstorage"
	"github.com/VictoriaMetrics/VictoriaMetrics/app/vlstorage/netselect"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/atomicutil"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/bytesutil"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/encoding"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/encoding/zstd"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/httpserver"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/logstorage"
	"github.com/VictoriaMetrics/metrics"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	jaegerServicesRequests = metrics.NewCounter(`vl_http_requests_total{path="/select/jaeger/api/services"}`)
	jaegerServicesDuration = metrics.NewSummary(`vl_http_request_duration_seconds{path="/select/jaeger/api/services"}`)

	jaegerOperationsRequests = metrics.NewCounter(`vl_http_requests_total{path="/select/jaeger/api/services/*/operations"}`)
	jaegerOperationsDuration = metrics.NewSummary(`vl_http_request_duration_seconds{path="/select/jaeger/api/services/*/operations"}`)

	jaegerTracesRequests = metrics.NewCounter(`vl_http_requests_total{path="/select/jaeger/api/traces"}`)
	jaegerTracesDuration = metrics.NewSummary(`vl_http_request_duration_seconds{path="/select/jaeger/api/traces"}`)

	jaegerTraceRequests = metrics.NewCounter(`vl_http_requests_total{path="/select/jaeger/api/traces/*"}`)
	jaegerTraceDuration = metrics.NewSummary(`vl_http_request_duration_seconds{path="/select/jaeger/api/traces/*"}`)

	jaegerDependenciesRequests = metrics.NewCounter(`vl_http_requests_total{path="/select/jaeger/api/dependencies"}`)
	jaegerDependenciesDuration = metrics.NewSummary(`vl_http_request_duration_seconds{path="/select/jaeger/api/dependencies"}`)
)

func RequestHandler(ctx context.Context, w http.ResponseWriter, r *http.Request) bool {
	httpserver.EnableCORS(w, r)
	startTime := time.Now()

	path := r.URL.Path
	if path == "/api/services" {
		jaegerServicesRequests.Inc()
		processQueryRequest()
		jaegerServicesDuration.UpdateDuration(startTime)
		return true
	} else if strings.HasPrefix(path, "/api/services/") && strings.HasSuffix(path, "/operations") {
		jaegerOperationsRequests.Inc()
		//logsql.ProcessFieldNamesRequest(ctx, w, r)
		jaegerOperationsDuration.UpdateDuration(startTime)
		return true
	} else if path == "/api/traces" {
		jaegerTracesRequests.Inc()
		// todo
		jaegerTracesDuration.UpdateDuration(startTime)
		return true
	} else if strings.HasPrefix(path, "/api/traces/") && len(path) > len("/api/traces/") {
		jaegerTraceRequests.Inc()
		// todo
		jaegerTraceDuration.UpdateDuration(startTime)
		return true
	} else if path == "/api/dependencies" {
		jaegerDependenciesRequests.Inc()
		// todo
		jaegerDependenciesDuration.UpdateDuration(startTime)
		return true
	}
	return false
}

func processGetServicesRequest(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	cp, err := getCommonParams(r)
	if err != nil {
		return err
	}

	// Write results
	w.Header().Set("Content-Type", "application/json")
	WriteValuesWithHitsJSON(w, fieldNames)

	sendBuf := func(bb *bytesutil.ByteBuffer) error {
		if len(bb.B) == 0 {
			return nil
		}

		data := bb.B
		if !cp.DisableCompression {
			bufLen := len(bb.B)
			bb.B = zstd.CompressLevel(bb.B, bb.B, 1)
			data = bb.B[bufLen:]
		}

		wLock.Lock()
		dataLenBuf = encoding.MarshalUint64(dataLenBuf[:0], uint64(len(data)))
		_, err := w.Write(dataLenBuf)
		if err == nil {
			_, err = w.Write(data)
		}
		wLock.Unlock()

		// Reset the sent buf
		bb.Reset()

		return err
	}

	var bufs atomicutil.Slice[bytesutil.ByteBuffer]

	var errGlobalLock sync.Mutex
	var errGlobal error

	writeBlock := func(workerID uint, db *logstorage.DataBlock) {
		if errGlobal != nil {
			return
		}

		bb := bufs.Get(workerID)

		bb.B = db.Marshal(bb.B)

		if len(bb.B) < 1024*1024 {
			// Fast path - the bb is too small to be sent to the client yet.
			return
		}

		// Slow path - the bb must be sent to the client.
		if err := sendBuf(bb); err != nil {
			errGlobalLock.Lock()
			if errGlobal != nil {
				errGlobal = err
			}
			errGlobalLock.Unlock()
		}
	}

	if err := vlstorage.RunQuery(ctx, cp.TenantIDs, cp.Query, writeBlock); err != nil {
		return err
	}
	if errGlobal != nil {
		return errGlobal
	}

	// Send the remaining data
	for _, bb := range bufs.GetSlice() {
		if err := sendBuf(bb); err != nil {
			return err
		}
	}

	return nil
}

type commonParams struct {
	TenantIDs []logstorage.TenantID
}

func getCommonParams(r *http.Request) (*commonParams, error) {
	tenantIDsStr := r.FormValue("tenant_ids")
	tenantIDs, err := logstorage.UnmarshalTenantIDs([]byte(tenantIDsStr))
	if err != nil {
		return nil, fmt.Errorf("cannot unmarshal tenant_ids=%q: %w", tenantIDsStr, err)
	}

	cp := &commonParams{
		TenantIDs: tenantIDs,
	}
	return cp, nil
}

func writeValuesWithHits(w http.ResponseWriter, vhs []logstorage.ValueWithHits, disableCompression bool) error {
	var b []byte
	for i := range vhs {
		b = vhs[i].Marshal(b)
	}

	if !disableCompression {
		b = zstd.CompressLevel(nil, b, 1)
	}

	w.Header().Set("Content-Type", "application/octet-stream")

	if _, err := w.Write(b); err != nil {
		return fmt.Errorf("cannot send response to the client: %w", err)
	}

	return nil
}

func getInt64FromRequest(r *http.Request, argName string) (int64, error) {
	s := r.FormValue(argName)
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("cannot parse %s=%q: %w", argName, s, err)
	}
	return n, nil
}
