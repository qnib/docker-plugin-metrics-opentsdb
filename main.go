package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
	"os"
	"io"
	"io/ioutil"
	"github.com/golang/protobuf/proto"

	"github.com/docker/go-plugins-helpers/sdk"
	"github.com/tv42/httpunix"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/prom2json"
	"github.com/qnib/prom2all"
	"github.com/docker/docker/client"
	"github.com/docker/docker/api/types"
	"context"
	"strings"
)

const (
	version = "0.1.0"
)

var (
	started bool
	mu 		sync.Mutex
	mfChan  chan *dto.MetricFamily

)

func Pusher() {
	host := os.Getenv("OPENTSDB_HOST")
	port := os.Getenv("OPENTSDB_PORT")
	addr := fmt.Sprintf("%s:%s", host, port)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		fmt.Println(err.Error())
	}
	for mf := range mfChan {
		f := prom2json.NewFamily(mf)
		hostname, err := os.Hostname()
		if err == nil {
			f.AddLabel("hostname", hostname)
		}
		msgs := prom2all.ToOpenTSDBv1(f)
		for _, msg := range msgs {
			if os.Getenv("DRY_RUN") != "true" {
				fmt.Fprintf(conn,  msg + "\n")
			} else {
				fmt.Printf(msg + "\n")
			}
		}
	}

}

func main() {
	fmt.Printf(">>>> Start plugin v%s\n", version)
	mfChan = make(chan *dto.MetricFamily, 1024)
	go Pusher()
	h := sdk.NewHandler(`{"Implements": ["MetricsCollector"]}`)
	handlers(&h)
	if err := h.ServeUnix("metrics", 0); err != nil {
		panic(err)
	}
}

func FetchContainers(cli *client.Client, ch chan<- *dto.MetricFamily) {
	fmt.Println(">> Start FetchContainers()\n")
	cnts, err := cli.ContainerList(context.Background(), types.ContainerListOptions{})
	if err != nil {
		log.Fatal(err)
	}
	for _, cnt := range cnts {
		resp, err := cli.ContainerStats(context.Background(), cnt.ID, false)
		if err != nil {
			fmt.Printf("Fetch container stats for %s went wrong: %s\n", cnt.ID, err.Error())
			continue
		}
		responseBody := resp.Body
		if responseBody != nil {
			defer responseBody.Close()
			defer ioutil.ReadAll(responseBody)
			//defer io.Copy(ioutil.Discard, responseBody)
		}

		if err != nil {
			fmt.Printf("Error to get stats info for %s\n", cnt.ID)
			continue
		}
		dec := json.NewDecoder(responseBody)
		var v *types.StatsJSON

		if err := dec.Decode(&v); err != nil {
			dec = json.NewDecoder(io.MultiReader(dec.Buffered(), responseBody))
			fmt.Printf("Error to decode stats info for '%s': %s\n", cnt.ID, err.Error())
			continue
		}
		TransformMetrics(v, cnt, ch)
	}
}

func TransformMetrics(v *types.StatsJSON, cnt types.Container, ch chan<- *dto.MetricFamily) {
	cntName := strings.Replace(strings.TrimLeft(cnt.Names[0], "/"), "_", "-",-1)
	fmt.Printf("Stats for '%s': %s\n", cntName, v.Read)
	labels := map[string]string{"container.id": cnt.ID, "container.name": cntName}
	mfs := []*dto.MetricFamily{}
	mfs = createCPUMetrics(v, mfs)
	mfs = createMemoryMetrics(v, mfs)
	for _, mf := range mfs {
		addLabels(mf, labels)
		ch <- mf
	}

}

func createMemoryMetrics(v *types.StatsJSON,mfs []*dto.MetricFamily) ([]*dto.MetricFamily) {
	mf := &dto.MetricFamily{
		Name: proto.String("memory.usage"),
		Type: dto.MetricType_COUNTER.Enum(),
		Metric: []*dto.Metric{{
				Label: []*dto.LabelPair{},
				Untyped: &dto.Untyped{
					Value: proto.Float64(float64(v.MemoryStats.Usage)),
				},
				TimestampMs: proto.Int64(v.Read.UnixNano()),
			}}}
	mfs = append(mfs, mf)
	mf = &dto.MetricFamily{
		Name: proto.String("memory.limit"),
		Type: dto.MetricType_GAUGE.Enum(),
		Metric: []*dto.Metric{{
			Label: []*dto.LabelPair{},
			Untyped: &dto.Untyped{
				Value: proto.Float64(float64(v.MemoryStats.Limit)),
			},
			TimestampMs: proto.Int64(v.Read.UnixNano()),
		}}}
	mfs = append(mfs, mf)
	return mfs
}

func createCPUMetrics(v *types.StatsJSON,mfs []*dto.MetricFamily) ([]*dto.MetricFamily) {
	mf := &dto.MetricFamily{
		Name: proto.String("cpu-usage"),
		Type: dto.MetricType_COUNTER.Enum(),
		Metric: []*dto.Metric{
			{
				Label: []*dto.LabelPair{&dto.LabelPair{
					Name:  proto.String("mode"),
					Value: proto.String("kernel"),
				}},
				Untyped: &dto.Untyped{
					Value: proto.Float64(float64(v.CPUStats.CPUUsage.UsageInKernelmode)),
				},
				TimestampMs: proto.Int64(v.Read.UnixNano()),
			},{
				Label: []*dto.LabelPair{&dto.LabelPair{
					Name:  proto.String("mode"),
					Value: proto.String("user"),
				}},
				Untyped: &dto.Untyped{
					Value: proto.Float64(float64(v.CPUStats.CPUUsage.UsageInUsermode)),
				},
				TimestampMs: proto.Int64(v.Read.UnixNano()),
			},{
				Label: []*dto.LabelPair{&dto.LabelPair{
					Name:  proto.String("mode"),
					Value: proto.String("system"),
				}},
				Untyped: &dto.Untyped{
					Value: proto.Float64(float64(v.CPUStats.SystemUsage)),
				},
				TimestampMs: proto.Int64(v.Read.UnixNano()),
			},
		},
	}
	mfs = append(mfs, mf)
	return mfs
}

func addLabels(mf *dto.MetricFamily, labels map[string]string) {
	for k,v := range labels {
		lb := &dto.LabelPair{
			Name:  proto.String(k),
			Value: proto.String(v),
		}
		for _, met := range mf.Metric {
			met.Label = append(met.Label, lb)
		}
	}
}

func PushForward() {
	ticker := time.NewTicker(time.Duration(2)*time.Second).C
	var cli *client.Client
	var err error
	if os.Getenv("CONTAINER_METRICS_ENABLE") == "true" {
		cli, err = client.NewEnvClient()
		if err != nil {
			log.Fatal(err)
		}
	}
	for {
		select {
		case <- ticker:
			u := &httpunix.Transport{
				DialTimeout:           100 * time.Millisecond,
				RequestTimeout:        500 * time.Millisecond,
				ResponseHeaderTimeout: 500 * time.Millisecond,
			}
			u.RegisterLocation("docker", "/run/docker/metrics.sock")

			var client = http.Client{
				Transport: u,
			}

			resp, err := client.Get("http+unix://docker/metrics")
			if err != nil {
				log.Fatal(err)
			}
			prom2json.ParseResponse(resp, mfChan)
			if os.Getenv("CONTAINER_METRICS_ENABLE") == "true" {
				FetchContainers(cli, mfChan)
			}
		}
	}
}

func handlers(h *sdk.Handler) {
	h.HandleFunc("/MetricsCollector.StartMetrics", func(w http.ResponseWriter, r *http.Request) {
		fmt.Println(">>>>>>> Got /MetricsCollector.StartMetrics")
		var err error
		defer func() {
			var res struct{ Err string }
			if err != nil {
				res.Err = err.Error()
			}
			json.NewEncoder(w).Encode(&res)
		}()
		mu.Lock()
		defer mu.Unlock()
		if ! started {
			started = true
			go PushForward()

		}
	})

	h.HandleFunc("/MetricsCollector.StopMetrics", func(w http.ResponseWriter, r *http.Request) {
		fmt.Println(">>>>>>> Got /MetricsCollector.StopMetrics")
		json.NewEncoder(w).Encode(map[string]string{})
	})
}
