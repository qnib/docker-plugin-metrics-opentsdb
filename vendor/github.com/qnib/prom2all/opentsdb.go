package prom2all


import (
	"github.com/prometheus/prom2json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"
	"reflect"
	"regexp"
)

// ToOpenTSDBv1 transforms the metrics(not yet Histograms/Summaries) to OpenTSDB line format (v1),
// returns an array of lines.
func ToOpenTSDBv1(f *prom2json.Family) []string {
	base := fmt.Sprintf("put %s %d", f.Name, time.Now().Unix())
	res := []string{}
	for _, item := range f.Metrics {
		switch item.(type) {
		case prom2json.Metric:
			m := item.(prom2json.Metric)
			val, err := strconv.ParseFloat(m.Value, 64)
			if err != nil {
				continue
			}
			lab, err := LabelToString(m.Labels)
			if err != nil {
				met := fmt.Sprintf("%s %f", base, val)
				res = append(res, met)
			} else {
				met := fmt.Sprintf("%s %f %s", base, val, strings.Join(lab, " "))
				res = append(res, met)
			}
		default:
			log.Printf("Type '%s' not yet implemented", reflect.TypeOf(item))
		}
	}
	return res
}

// LabelToString consumes a k/v map and returns a sanitized []string{} with key=val pairs.
// In case the map is empty or all k/v pairs fail the sanitization test, an error is return.
func LabelToString(inp map[string]string) (lab []string, err error) {
	if len(inp) == 0 {
		return nil, errors.New("amp is empty, therefore no string for you")
	}
	for k, v := range inp {
		tag, err := SanitizeTags(k, v)
		if err != nil {
			log.Printf(err.Error())
			continue
		}
		lab = append(lab, tag)
	}
	if len(lab) == 0 {
		return nil, errors.New("all k/v pairs failed the sanitization test")
	}
	return
}

// SanitizeTags checks whether the tag is compliant with the rules defined in
// http://opentsdb.net/docs/build/html/user_guide/writing.html#metrics-and-tags.
// For starters only `^[a-zA-Z0-9\-\./]+$` is allowed.
// TODO: Add Unicode letters
func SanitizeTags(k, v string) (tag string, err error) {
	// a to z, A to Z, 0 to 9, -, _, ., /
	r := regexp.MustCompile(`^[a-zA-Z0-9\-\./]+$`)
	if !r.MatchString(k) || !r.MatchString(v) {
		return "", fmt.Errorf(`Did not match '[a-zA-Z\-\./]+': %s=%s`, k, v)
	}
	tag = fmt.Sprintf("%s=%s", k, v)
	return
}
