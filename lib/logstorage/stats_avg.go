package logstorage

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/VictoriaMetrics/VictoriaMetrics/lib/bytesutil"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/encoding"
)

type statsAvg struct {
	fields []string
}

func (sa *statsAvg) String() string {
	return "avg(" + statsFuncFieldsToString(sa.fields) + ")"
}

func (sa *statsAvg) updateNeededFields(neededFields fieldsSet) {
	updateNeededFieldsForStatsFunc(neededFields, sa.fields)
}

func (sa *statsAvg) newStatsProcessor(a *chunkedAllocator) statsProcessor {
	return a.newStatsAvgProcessor()
}

type statsAvgProcessor struct {
	sum   float64
	count uint64
}

func (sap *statsAvgProcessor) updateStatsForAllRows(sf statsFunc, br *blockResult) int {
	sa := sf.(*statsAvg)
	fields := sa.fields
	if len(fields) == 0 {
		// Scan all the columns
		for _, c := range br.getColumns() {
			f, count := c.sumValues(br)
			sap.sum += f
			sap.count += uint64(count)
		}
	} else {
		// Scan the requested columns
		for _, field := range fields {
			c := br.getColumnByName(field)
			f, count := c.sumValues(br)
			sap.sum += f
			sap.count += uint64(count)
		}
	}
	return 0
}

func (sap *statsAvgProcessor) updateStatsForRow(sf statsFunc, br *blockResult, rowIdx int) int {
	sa := sf.(*statsAvg)
	fields := sa.fields
	if len(fields) == 0 {
		// Scan all the fields for the given row
		for _, c := range br.getColumns() {
			f, ok := c.getFloatValueAtRow(br, rowIdx)
			if ok {
				sap.sum += f
				sap.count++
			}
		}
	} else {
		// Scan only the given fields for the given row
		for _, field := range fields {
			c := br.getColumnByName(field)
			f, ok := c.getFloatValueAtRow(br, rowIdx)
			if ok {
				sap.sum += f
				sap.count++
			}
		}
	}
	return 0
}

func (sap *statsAvgProcessor) mergeState(_ *chunkedAllocator, _ statsFunc, sfp statsProcessor) {
	src := sfp.(*statsAvgProcessor)
	sap.sum += src.sum
	sap.count += src.count
}

func (sap *statsAvgProcessor) exportState(dst []byte, _ <-chan struct{}) []byte {
	dst = marshalFloat64(dst, sap.sum)
	dst = encoding.MarshalVarUint64(dst, sap.count)
	return dst
}

func (sap *statsAvgProcessor) importState(src []byte, _ <-chan struct{}) (int, error) {
	if len(src) < 8 {
		return 0, fmt.Errorf("cannot unmarshal sum from %d bytes; need 8 bytes", len(src))
	}
	sap.sum = unmarshalFloat64(bytesutil.ToUnsafeString(src))
	src = src[8:]

	count, n := encoding.UnmarshalVarUint64(src)
	if n <= 0 {
		return 0, fmt.Errorf("cannot unmarshal count")
	}
	sap.count = count
	src = src[n:]

	if len(src) > 0 {
		return 0, fmt.Errorf("unexpected tail left; len(tail)=%d", len(src))
	}

	return 0, nil
}

func (sap *statsAvgProcessor) finalizeStats(_ statsFunc, dst []byte, _ <-chan struct{}) []byte {
	avg := sap.sum / float64(sap.count)
	return strconv.AppendFloat(dst, avg, 'f', -1, 64)
}

func parseStatsAvg(lex *lexer) (*statsAvg, error) {
	fields, err := parseStatsFuncFields(lex, "avg")
	if err != nil {
		return nil, err
	}
	sa := &statsAvg{
		fields: fields,
	}
	return sa, nil
}

func parseStatsFuncFields(lex *lexer, funcName string) ([]string, error) {
	if !lex.isKeyword(funcName) {
		return nil, fmt.Errorf("unexpected func; got %q; want %q", lex.token, funcName)
	}
	lex.nextToken()
	fields, err := parseFieldNamesInParens(lex)
	if err != nil {
		return nil, fmt.Errorf("cannot parse %q args: %w", funcName, err)
	}
	if len(fields) == 0 || slices.Contains(fields, "*") {
		fields = nil
	}
	return fields, nil
}

func statsFuncFieldsToString(fields []string) string {
	if len(fields) == 0 {
		return "*"
	}
	return fieldsToString(fields)
}

func fieldsToString(fields []string) string {
	a := make([]string, len(fields))
	for i, f := range fields {
		a[i] = quoteTokenIfNeeded(f)
	}
	return strings.Join(a, ", ")
}

func updateNeededFieldsForStatsFunc(neededFields fieldsSet, fields []string) {
	if len(fields) == 0 {
		neededFields.add("*")
	}
	neededFields.addFields(fields)
}
