package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"netint_vpu_exporter/internal/config"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/alecthomas/kingpin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/version"
	"github.com/rs/zerolog/log"
)

type Metadata struct {
	INDEX      int
	LOAD       int
	MODEL_LOAD int
	INST       int
	MEM        int
	SHARE_MEM  int
	P2P_MEM    int
	DEVICE     string
	NAMESPACE  string
	TEMP       int
}
type Devices struct {
	Decoders []Metadata
	Encoders []Metadata
	Scalers  []Metadata
	AIs      []Metadata
}

type PrometheusCounters struct {
	DecoderCounters map[string]*prometheus.CounterVec
	EncoderCounters map[string]*prometheus.CounterVec
	ScalerCounters  map[string]*prometheus.CounterVec
	AICounters      map[string]*prometheus.CounterVec
}

type NvmeMetadata struct {
	CriticalWarning    int `json:"critical_warning"`
	Temperature        int `json:"temperature"`
	AvailSpare         int `json:"avail_spare"`
	SpareThresh        int `json:"spare_thresh"`
	PercentUsed        int `json:"percent_used"`
	DataUnitsRead      int `json:"data_units_read"`
	DataUnitsWritten   int `json:"data_units_written"`
	HostReadCommands   int `json:"host_read_commands"`
	HostWriteCommands  int `json:"host_write_commands"`
	ControllerBusyTime int `json:"controller_busy_time"`
	PowerCycles        int `json:"power_cycles"`
	PowerOnHours       int `json:"power_on_hours"`
	UnsafeShutdowns    int `json:"unsafe_shutdowns"`
	MediaErrors        int `json:"media_errors"`
	NumErrLogEntries   int `json:"num_err_log_entries"`
	WarningTempTime    int `json:"warning_temp_time"`
	CriticalCompTime   int `json:"critical_comp_time"`
	TemperatureSensor1 int `json:"temperature_sensor_1"`
	TemperatureSensor2 int `json:"temperature_sensor_2"`
	ThmTemp1TransCount int `json:"thm_temp1_trans_count"`
	ThmTemp2TransCount int `json:"thm_temp2_trans_count"`
	ThmTemp1TotalTime  int `json:"thm_temp1_total_time"`
	ThmTemp2TotalTime  int `json:"thm_temp2_total_time"`
}

var METRIC_LABELS = []string{
	"LOAD", "MODEL_LOAD", "INST", "MEM", "SHARE_MEM", "P2P_MEM", "TEMP",
}

func (c *Metadata) GetVal(field string) (int, error) {
	fields := map[string]int{
		"LOAD":       c.LOAD,
		"MODEL_LOAD": c.MODEL_LOAD,
		"INST":       c.INST,
		"MEM":        c.MEM,
		"SHARE_MEM":  c.SHARE_MEM,
		"P2P_MEM":    c.P2P_MEM,
		"TEMP":       c.TEMP,
	}

	value := fields[field]
	if value < 0 {
		return 0, fmt.Errorf("invalid value for field %s", field)
	}
	return value, nil
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
	prometheusCounters := PrometheusCounters{
		DecoderCounters: make(map[string]*prometheus.CounterVec),
		EncoderCounters: make(map[string]*prometheus.CounterVec),
		ScalerCounters:  make(map[string]*prometheus.CounterVec),
		AICounters:      make(map[string]*prometheus.CounterVec),
	}
	initCounters(registry, &prometheusCounters)

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

func initCounters(registry *prometheus.Registry, prometheusCounters *PrometheusCounters) {

	for _, field := range METRIC_LABELS {
		prometheusCounters.DecoderCounters[field] = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: fmt.Sprintf("netint_decoder_%s_total", field),
				Help: fmt.Sprintf("Total %s for decoder", field),
			},
			[]string{"index", "device"},
		)
		prometheusCounters.EncoderCounters[field] = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: fmt.Sprintf("netint_encoder_%s_total", field),
				Help: fmt.Sprintf("Total %s for encoder", field),
			},
			[]string{"index", "device"},
		)
		prometheusCounters.ScalerCounters[field] = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: fmt.Sprintf("netint_scaler_%s_total", field),
				Help: fmt.Sprintf("Total %s for scaler", field),
			},
			[]string{"index", "device"},
		)
		prometheusCounters.AICounters[field] = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: fmt.Sprintf("netint_ai_%s_total", field),
				Help: fmt.Sprintf("Total %s for AI", field),
			},
			[]string{"index", "device"},
		)
		registry.MustRegister(prometheusCounters.DecoderCounters[field])
		registry.MustRegister(prometheusCounters.EncoderCounters[field])
		registry.MustRegister(prometheusCounters.ScalerCounters[field])
		registry.MustRegister(prometheusCounters.AICounters[field])
	}
	log.Info().Msg("Counters initialized")
}

func runCollector(prometheusCounters *PrometheusCounters) {
	cmd := exec.Command("sh", "-c", "ni_rsrc_mon -o text")
	output, err := cmd.Output()
	if err != nil {
		log.Error().Err(err).Msg("Error executing command")
		return
	}
	devices, err := parseStatsFromString(string(output))
	if err != nil {
		log.Error().Err(err).Msg("Error al analizar la salida:")
		return
	}
	fmt.Printf("Devices: %+v\n", devices)
	updateTemperatureForDevices(&devices.Decoders)
	updateTemperatureForDevices(&devices.Encoders)
	updateTemperatureForDevices(&devices.Scalers)
	updateTemperatureForDevices(&devices.AIs)
	updateMetrics(devices.Decoders, prometheusCounters.DecoderCounters)
	updateMetrics(devices.Encoders, prometheusCounters.EncoderCounters)
	updateMetrics(devices.Scalers, prometheusCounters.ScalerCounters)
	updateMetrics(devices.AIs, prometheusCounters.AICounters)

	log.Debug().Msg("Metrics updated")
}

func updateMetrics(components []Metadata, counters map[string]*prometheus.CounterVec) {
	for _, c := range components {
		labels := prometheus.Labels{
			"index":  fmt.Sprintf("%d", c.INDEX),
			"device": c.DEVICE,
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

func parseStatsFromString(output string) (*Devices, error) {
	scanner := bufio.NewScanner(strings.NewReader(output))
	var devices Devices
	var current *[]Metadata
	var bestHeader bool

	for scanner.Scan() {
		line := scanner.Text()

		switch {
		case strings.HasPrefix(line, "Num decoders:"):
			current = &devices.Decoders
			bestHeader = false
		case strings.HasPrefix(line, "Num encoders:"):
			current = &devices.Encoders
			bestHeader = false
		case strings.HasPrefix(line, "Num scalers:"):
			current = &devices.Scalers
			bestHeader = false
		case strings.HasPrefix(line, "Num AIs:"):
			current = &devices.AIs
			bestHeader = false
		case strings.Contains(line, "BEST INDEX"):
			bestHeader = true
			continue
		case strings.HasPrefix(line, "INDEX"):
			bestHeader = false
			continue
		case line == "" || strings.HasPrefix(line, "*"):
			continue
		default:
			if current == nil {
				continue
			}
			fields := strings.Fields(line)

			if bestHeader {
				offset := 0
				if fields[0] == "L" || fields[0] == "" {
					offset = 1
				}

				if len(fields) < offset+7 {
					log.Warn().Msgf("Skipping malformed BEST line: %s", line)
					continue
				}

				index, _ := strconv.Atoi(fields[offset+0])
				load, _ := strconv.Atoi(fields[offset+1])
				modelLoad, _ := strconv.Atoi(fields[offset+2])
				mem, _ := strconv.Atoi(fields[offset+3])
				inst, _ := strconv.Atoi(fields[offset+4])
				device := fields[offset+5]
				namespace := fields[offset+6]

				*current = append(*current, Metadata{
					INDEX:      index,
					LOAD:       load,
					MODEL_LOAD: modelLoad,
					INST:       inst,
					MEM:        mem,
					DEVICE:     device,
					NAMESPACE:  namespace,
					SHARE_MEM:  0,
					P2P_MEM:    0,
				})
			} else {
				if len(fields) < 9 {
					log.Warn().Msgf("Skipping malformed standard line: %s", line)
					continue
				}
				index, _ := strconv.Atoi(fields[0])
				load, _ := strconv.Atoi(fields[1])
				modelLoad, _ := strconv.Atoi(fields[2])
				inst, _ := strconv.Atoi(fields[3])
				mem, _ := strconv.Atoi(fields[4])
				shareMem, _ := strconv.Atoi(fields[5])
				p2pMem, _ := strconv.Atoi(fields[6])
				device := fields[7]
				namespace := fields[8]

				*current = append(*current, Metadata{
					INDEX:      index,
					LOAD:       load,
					MODEL_LOAD: modelLoad,
					INST:       inst,
					MEM:        mem,
					SHARE_MEM:  shareMem,
					P2P_MEM:    p2pMem,
					DEVICE:     device,
					NAMESPACE:  namespace,
				})
			}
		}
	}
	return &devices, scanner.Err()
}

func updateTemperatureForDevices(devices *[]Metadata) {
	for i := range *devices {
		temp, err := getNVMeTemperature((*devices)[i].DEVICE)
		fmt.Printf("Device: %s, Temperature: %d\n", (*devices)[i].DEVICE, temp)
		if err != nil {
			log.Error().Err(err).Msgf("Error getting temperature for device %s", (*devices)[i].DEVICE)
			continue
		}
		(*devices)[i].TEMP = temp - 273
	}
}

func getNVMeTemperature(deviceName string) (int, error) {
	cmd := exec.Command("nvme", "smart-log", deviceName, "-o", "json")
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("error executing nvme smart-log: %v", err)
	}
	var nvmeMetadata NvmeMetadata
	err = json.Unmarshal(output, &nvmeMetadata)
	if err != nil {
		return 0, fmt.Errorf("error unmarshaling nvme smart-log output: %v", err)
	}

	return nvmeMetadata.Temperature, nil
}
