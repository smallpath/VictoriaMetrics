package jaeger

import (
	"context"
	"fmt"
	"github.com/VictoriaMetrics/VictoriaMetrics/app/vlstorage"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/httpserver"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/logger"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/logstorage"
	"github.com/VictoriaMetrics/metrics"
	"net/http"
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
		processGetServicesRequest(ctx, w, r)
		jaegerServicesDuration.UpdateDuration(startTime)
		return true
	} else if strings.HasPrefix(path, "/api/services/") && strings.HasSuffix(path, "/operations") {
		jaegerOperationsRequests.Inc()
		processGetOperationsRequest(ctx, w, r)
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

func processGetServicesRequest(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	cp, err := getCommonParams(r)
	if err != nil {
		httpserver.Errorf(w, r, "incorrect query params: %s", err)
		return
	}

	qStr := "*"
	q, err := logstorage.ParseQueryAtTimestamp(qStr, time.Now().UnixNano())
	if err != nil {
		httpserver.Errorf(w, r, "cannot parse query [%s]: %s", qStr, err)
		return
	}
	q.AddTimeFilter(0, time.Now().UnixNano())
	logger.Infof("GetServices StreamFieldValues query: %s", q.String())
	serviceHits, err := vlstorage.GetStreamFieldValues(ctx, cp.TenantIDs, q, ResourceAttrPrefix+"service.name", uint64(1000))
	if err != nil {
		httpserver.Errorf(w, r, "cannot parse query [%s]: %s", qStr, err)
		return
	}

	serviceList := make([]string, 0, len(serviceHits))
	for i := range serviceHits {
		serviceList = append(serviceList, serviceHits[i].Value)
	}
	// Write results
	w.Header().Set("Content-Type", "application/json")
	WriteGetServicesResponse(w, serviceList)
	return
}

func processGetOperationsRequest(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	cp, err := getCommonParams(r)
	if err != nil {
		httpserver.Errorf(w, r, "incorrect query params: %s", err)
		return
	}

	paths := strings.Split(strings.TrimSuffix(r.URL.Path, "/"), "/")
	if len(paths) < 4 {
		httpserver.Errorf(w, r, "incorrect query path [%s]", r.URL.Path)
		return
	}
	serviceName := paths[len(paths)-2]

	qStr := fmt.Sprintf("_stream:{%s=\"%s\"}", ResourceAttrPrefix+"service.name", serviceName) // todo spankind filter
	q, err := logstorage.ParseQueryAtTimestamp(qStr, time.Now().UnixNano())
	if err != nil {
		httpserver.Errorf(w, r, "cannot parse query [%s]: %s", qStr, err)
		return
	}
	logger.Infof("GetOperations StreamFieldValues query: %s", q.String())
	operationHits, err := vlstorage.GetStreamFieldValues(ctx, cp.TenantIDs, q, Name, uint64(1000))
	if err != nil {
		httpserver.Errorf(w, r, "get operation hits error: %s", err)
		return
	}

	operationList := make([]string, 0, len(operationHits))
	for i := range operationHits {
		operationList = append(operationList, operationHits[i].Value)
	}

	// Write results
	w.Header().Set("Content-Type", "application/json")
	WriteGetOperationsResponse(w, operationList)
	return
}

// traceQueryParameters defines the parameters for querying a batch of traces from the query service.
type traceQueryParameters struct {
	ServiceName   string
	OperationName string
	Tags          map[string]string
	StartTimeMin  time.Time
	StartTimeMax  time.Time
	DurationMin   time.Duration
	DurationMax   time.Duration
	NumTraces     int
	RawTraces     bool
}

type row struct {
	timestamp int64
	fields    []logstorage.Field
}

func parseTraceQueryParams(ctx context.Context, r *http.Request) (*traceQueryParameters, error) {
	return nil, nil
}

func processGetTracesRequest(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	defer func() {
		logger.Infof("FindTraces finished in %dms", time.Since(start).Milliseconds())
	}()

	cp, err := getCommonParams(r)
	if err != nil {
		httpserver.Errorf(w, r, "incorrect query params: %s", err)
		return
	}

	query, err := parseTraceQueryParams(ctx, r)
	if err != nil {
		httpserver.Errorf(w, r, "incorrect trace query params: %s", err)
	}

	traceIDs, err := processGetTraceIDsRequest(ctx, cp, query)
	if err != nil {
		httpserver.Errorf(w, r, "get trace id error: %w", err)
		return
	}
	if len(traceIDs) == 0 {
		// Write results
		w.Header().Set("Content-Type", "application/json")
		WriteGetOperationsResponse(w, traceIDs)
		return
	}

	qStr := fmt.Sprintf(TraceId+":in(%s)", strings.Join(traceIDs, ","))
	q, err := logstorage.ParseQueryAtTimestamp(qStr, time.Now().UnixNano())
	if err != nil {
		httpserver.Errorf(w, r, "cannot parse query [%s]: %s", qStr, err)
		return
	}
	q.AddTimeFilter(query.StartTimeMin.UnixNano(), query.StartTimeMax.UnixNano())

	var rows []row
	var rowsLock sync.Mutex
	writeBlock := func(_ uint, timestamps []int64, columns []logstorage.BlockColumn) {
		clonedColumnNames := make([]string, len(columns))
		for i, c := range columns {
			clonedColumnNames[i] = strings.Clone(c.Name)
		}

		for i, timestamp := range timestamps {
			fields := make([]logstorage.Field, 0, len(columns))
			for j := range columns {
				if columns[j].Values[i] != "" {
					fields = append(fields, logstorage.Field{Name: clonedColumnNames[j], Value: strings.Clone(columns[j].Values[i])})
				}
			}

			rowsLock.Lock()
			rows = append(rows, row{
				timestamp: timestamp,
				fields:    fields,
			})
			rowsLock.Unlock()
		}
	}
	logger.Infof("FindTraces query: %s", q.String())
	if err = vlstorage.RunQuery(ctx, cp., q, writeBlock); err != nil {
		return nil, err
	}
	tracesMap := make(map[string]*model.Trace)
	traces := make([]*model.Trace, len(traceIDs), len(traceIDs))
	for i := range traceIDs {
		traces[i] = &model.Trace{}
		tracesMap[traceIDs[i]] = traces[i]
	}

	for i := range rows {
		sp, err := jaeger.FieldsToSpan(rows[i].fields)
		if err != nil {
			logger.Errorf("cannot unmarshal log fields [%v] to span: %s", rows[i].fields, err)
			continue
		}

		tracesMap[sp.TraceID.String()].Spans = append(tracesMap[sp.TraceID.String()].Spans, sp)
	}
	return traces, nil
}

func processGetTraceIDsRequest(ctx context.Context, cp *commonParams, query *traceQueryParameters) ([]string, error) {
	start := time.Now()
	defer func() {
		logger.Infof("FindTraceIDs finished in %dms", time.Since(start).Milliseconds())
	}()
	qStr := ""
	if query.ServiceName != "" {
		qStr += fmt.Sprintf("AND _stream:{"+ResourceAttrPrefix+"service.name"+"=\"%s\"} ", query.ServiceName)
	}
	if query.OperationName != "" {
		qStr += fmt.Sprintf("AND _stream:{"+Name+"=\"%s\"} ", query.OperationName)
	}

	if len(query.Tags) > 0 {
		for k, v := range query.Tags {
			qStr += fmt.Sprintf(`AND "`+SpanAttrPrefix+`%s":=%s `, k, v)
		}
	}
	if query.DurationMin > 0 {
		qStr += fmt.Sprintf("AND "+Duration+":>%d ", query.DurationMin.Nanoseconds())
	}
	if query.DurationMax > 0 {
		qStr += fmt.Sprintf("AND duration:<%d ", query.DurationMax.Nanoseconds())
	}
	qStr = strings.TrimLeft(qStr+" | last 1 by (_time) partition by ("+TraceId+") | fields _time, "+TraceId+" | sort by (_time) desc", "AND ")

	logger.Infof("FindTraceIDs query debug: %s", qStr)
	q, err := logstorage.ParseQueryAtTimestamp(qStr, query.StartTimeMax.UnixNano())
	if err != nil {
		return nil, fmt.Errorf("cannot parse query [%s]: %s", qStr, err)
	}
	q.AddPipeLimit(uint64(query.NumTraces))

	traceIDs, err := findTraceIDsSplitTimeRange(ctx, q, cp, query.StartTimeMin, query.StartTimeMax, query.NumTraces)
	if err != nil {
		return nil, err
	}

	return traceIDs, nil
}

// findTraceIDsSplitTimeRange try to search from the nearest time range of the end time.
// if the result already met requirement of `limit`, return.
// otherwise, amplify the time range to 5x and search again, until the start time exceed the input.
func findTraceIDsSplitTimeRange(ctx context.Context, q *logstorage.Query, cp *commonParams, startTime, endTime time.Time, limit int) ([]string, error) {
	step := time.Minute
	startTimeCurrent := endTime.Add(-step)
	traceIDList := make([]string, 0, 10)
	writeBlock := func(_ uint, db *logstorage.DataBlock) {
		columns := db.Columns
		for i := range columns {
			if columns[i].Name == "trace_id" {
				for _, v := range columns[i].Values {
					traceIDList = append(traceIDList, v)
				}

			}
		}
	}

	for startTimeCurrent.After(startTime) {
		qClone := q.CloneWithTimeFilter(endTime.UnixNano(), startTimeCurrent.UnixNano(), endTime.UnixNano())
		logger.Infof("FindTraces query: %s", qClone.String())
		if err := vlstorage.RunQuery(ctx, cp.TenantIDs, qClone, writeBlock); err != nil {
			return nil, err
		}
		if len(traceIDList) == limit {
			return traceIDList, nil
		}
		traceIDList = traceIDList[:0]
		step *= 5
		startTimeCurrent = startTimeCurrent.Add(-step)
	}

	// one last try with input time range
	qClone := q.CloneWithTimeFilter(endTime.UnixNano(), startTimeCurrent.UnixNano(), endTime.UnixNano())
	logger.Infof("FindTraces query: %s", qClone.String())
	if err := vlstorage.RunQuery(ctx, cp.TenantIDs, qClone, writeBlock); err != nil {
		return nil, err
	}
	return traceIDList, nil
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
