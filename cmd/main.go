package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"netint_vpu_exporter/internal/config"
	"os/exec"
	"time"

	"github.com/alecthomas/kingpin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/version"
	"github.com/rs/zerolog/log"
)

type Metadata struct {
	NUMBER     int    `json:"NUMBER"`
	INDEX      int    `json:"INDEX"`
	LOAD       int    `json:"LOAD"`
	MODEL_LOAD int    `json:"MODEL_LOAD"`
	FW_LOAD    int    `json:"FW_LOAD"`
	INST       int    `json:"INST"`
	MAX_INST   int    `json:"MAX_INST"`
	MEM        int    `json:"MEM"`
	SHARE_MEM  int    `json:"SHARE_MEM"`
	P2P_MEM    int    `json:"P2P_MEM"`
	DEVICE     string `json:"DEVICE"`
	NAMESPACE  string `json:"NAMESPACE"`
	NUMA_NODE  int    `json:"NUMA_NODE"`
	PCIE_ADDR  string `json:"PCIE_ADDR"`
}

func (c *Metadata) GetVal(field string) (int, error) {
	fields := map[string]int{
		"LOAD":       c.LOAD,
		"MODEL_LOAD": c.MODEL_LOAD,
		"FW_LOAD":    c.FW_LOAD,
		"INST":       c.INST,
		"MAX_INST":   c.MAX_INST,
		"MEM":        c.MEM,
		"SHARE_MEM":  c.SHARE_MEM,
		"P2P_MEM":    c.P2P_MEM,
		"NUMBER":     c.NUMBER,
		"NUMA_NODE":  c.NUMA_NODE,
	}

	value := fields[field]
	if value < 0 {
		return 0, fmt.Errorf("invalid value for field %s", field)
	}
	return value, nil
}

type Devices struct {
	Decoders  []Metadata `json:"decoders"`
	Encoders  []Metadata `json:"encoders"`
	Uploaders []Metadata `json:"uploaders"`
	Scalers   []Metadata `json:"scalers"`
	AIs       []Metadata `json:"AIs"`
	Nvmes     []Metadata `json:"nvmes"`
}

type PromehtheusCounters struct {
	DecoderCounters  map[string]*prometheus.CounterVec
	EncoderCounters  map[string]*prometheus.CounterVec
	UploaderCounters map[string]*prometheus.CounterVec
	ScalerCounters   map[string]*prometheus.CounterVec
	AICounters       map[string]*prometheus.CounterVec
}

var METRIC_LABELS = []string{
	"LOAD", "MODEL_LOAD", "FW_LOAD", "INST", "MAX_INST",
	"MEM", "SHARE_MEM", "P2P_MEM", "NUMBER", "NUMA_NODE",
}

var (
	listenAddr = kingpin.Flag("web.listen-address", "Address to listen on for web interface and telemetry.").Default(":9836").String()
	logLevel   = kingpin.Flag("log.level", "Only log messages with the given severity or above. One of: [debug, info, warn, error]").Default("info").String()
)

func main() {
	kingpin.Version(version.Print("netint_exporter"))
	kingpin.Parse()
	config.ConfigureZeroLog(*logLevel)

	registry := prometheus.NewRegistry()
	prometheusCounters := PromehtheusCounters{
		DecoderCounters:  make(map[string]*prometheus.CounterVec),
		EncoderCounters:  make(map[string]*prometheus.CounterVec),
		UploaderCounters: make(map[string]*prometheus.CounterVec),
		ScalerCounters:   make(map[string]*prometheus.CounterVec),
		AICounters:       make(map[string]*prometheus.CounterVec),
	}
	initCounters(registry, &prometheusCounters)

	//channels, singals, for, etc

	go func() {
		for {
			runCollector(&prometheusCounters)
			time.Sleep(time.Millisecond * 500)
		}
	}()

	http.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{
		EnableOpenMetrics:                   true,
		EnableOpenMetricsTextCreatedSamples: true,
	}))

	log.Info().Msgf("Starting server on port %s", *listenAddr)
	log.Fatal().Err(http.ListenAndServe(*listenAddr, nil)).Msg("Error starting server")
}

func initCounters(registry *prometheus.Registry, prometheusCounters *PromehtheusCounters) {

	for _, field := range METRIC_LABELS {
		prometheusCounters.DecoderCounters[field] = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: fmt.Sprintf("netint_decoder_%s_total", field),
				Help: fmt.Sprintf("Total %s for decoder", field),
			},
			[]string{"index", "device", "pcie_addr"},
		)
		prometheusCounters.EncoderCounters[field] = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: fmt.Sprintf("netint_encoder_%s_total", field),
				Help: fmt.Sprintf("Total %s for encoder", field),
			},
			[]string{"index", "device", "pcie_addr"},
		)
		prometheusCounters.UploaderCounters[field] = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: fmt.Sprintf("netint_uploader_%s_total", field),
				Help: fmt.Sprintf("Total %s for uploader", field),
			},
			[]string{"index", "device", "pcie_addr"},
		)
		prometheusCounters.ScalerCounters[field] = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: fmt.Sprintf("netint_scaler_%s_total", field),
				Help: fmt.Sprintf("Total %s for scaler", field),
			},
			[]string{"index", "device", "pcie_addr"},
		)
		prometheusCounters.AICounters[field] = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: fmt.Sprintf("netint_ai_%s_total", field),
				Help: fmt.Sprintf("Total %s for AI", field),
			},
			[]string{"index", "device", "pcie_addr"},
		)
		registry.MustRegister(prometheusCounters.DecoderCounters[field])
		registry.MustRegister(prometheusCounters.EncoderCounters[field])
		registry.MustRegister(prometheusCounters.UploaderCounters[field])
		registry.MustRegister(prometheusCounters.ScalerCounters[field])
		registry.MustRegister(prometheusCounters.AICounters[field])
	}
	log.Info().Msg("Counters initialized")
}

func runCollector(promehtheusCounters *PromehtheusCounters) {
	cmd := exec.Command("sh", "-c", " ni_rsrc_mon -o json1 | sed '1d;2d;$d'")
	output, err := cmd.Output()
	if err != nil {
		log.Error().Err(err).Msg("Error executing command")
		return
	}
	var devices Devices
	err = json.Unmarshal(output, &devices)
	if err != nil {
		log.Error().Err(err).Msg("Error unmarshaling JSON")
		return
	}

	updateMetrics(devices.Decoders, promehtheusCounters.DecoderCounters)
	updateMetrics(devices.Encoders, promehtheusCounters.EncoderCounters)
	updateMetrics(devices.Uploaders, promehtheusCounters.UploaderCounters)
	updateMetrics(devices.Scalers, promehtheusCounters.ScalerCounters)
	updateMetrics(devices.AIs, promehtheusCounters.AICounters)
	log.Debug().Msg("Metrics updated")
}

func updateMetrics(components []Metadata, counters map[string]*prometheus.CounterVec) {
	for _, c := range components {
		labels := prometheus.Labels{
			"index":     fmt.Sprintf("%d", c.INDEX),
			"device":    c.DEVICE,
			"pcie_addr": c.PCIE_ADDR,
		}

		for _, field := range METRIC_LABELS {
			value, err := c.GetVal(field)
			if err != nil {
				log.Error().Err(err).Msgf("Error getting value for field %s", field)
				continue
			}
			counters[field].With(labels).Add(float64(value))
		}
	}
}
