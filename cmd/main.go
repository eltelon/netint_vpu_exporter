package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"netint_vpu_exporter/internal/config"
	"os"
	"os/exec"
	"path/filepath"
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
	DecoderCounters map[string]*prometheus.GaugeVec
	EncoderCounters map[string]*prometheus.GaugeVec
	ScalerCounters  map[string]*prometheus.GaugeVec
	AICounters      map[string]*prometheus.GaugeVec
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
	listenAddr     = kingpin.Flag("web.listen-address", "Address to listen on for web interface and telemetry.").Default(":9836").String()
	logLevel       = kingpin.Flag("log.level", "Only log messages with the given severity or above. One of: [debug, info, warn, error]").Default("info").String()
	sysloadGauge   *prometheus.GaugeVec
	channelXDevice *prometheus.GaugeVec
)

func main() {
	kingpin.Version(version.Print("netint_exporter"))
	kingpin.Parse()
	config.ConfigureZeroLog(*logLevel)

	registry := prometheus.NewRegistry()
	sysloadGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "netint_encoder_sysload",
			Help: "System load average of the encoder host",
		},
		[]string{"interval"},
	)
	channelXDevice = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "netint_encoder_channel_device",
			Help: "channel to device mapping",
		},
		[]string{"channel", "device"},
	)
	registry.MustRegister(sysloadGauge)
	registry.MustRegister(channelXDevice)
	prometheusCounters := PrometheusCounters{
		DecoderCounters: make(map[string]*prometheus.GaugeVec),
		EncoderCounters: make(map[string]*prometheus.GaugeVec),
		ScalerCounters:  make(map[string]*prometheus.GaugeVec),
		AICounters:      make(map[string]*prometheus.GaugeVec),
	}
	initCounters(registry, &prometheusCounters)

	go func() {
		for {
			runCollector(&prometheusCounters)
			time.Sleep(time.Millisecond * 500)
		}
	}()

	go func() {
		for {
			files, err := filepath.Glob("/home/scripts/ffmpeg-*.sh")
			if err != nil {
				log.Error().Err(err).Msg("Error finding ffmpeg scripts")
				continue
			}
			for _, file := range files {
				vpu, err := extractVPUValue(file)
				if err != nil {
					channel := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(file), "ffmpeg-"), ".sh")
					channelXDevice.WithLabelValues(channel, "CPU").Set(1)
					continue
				}
				vpu = strings.Trim(vpu, "\"")
				channel := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(file), "ffmpeg-"), ".sh")
				channelXDevice.WithLabelValues(channel, "/dev/nvme"+vpu).Set(1)
			}
			time.Sleep(500 * time.Millisecond)
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
		prometheusCounters.DecoderCounters[field] = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: fmt.Sprintf("netint_decoder_%s_total", field),
				Help: fmt.Sprintf("Total %s for decoder", field),
			},
			[]string{"index", "device"},
		)
		prometheusCounters.EncoderCounters[field] = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: fmt.Sprintf("netint_encoder_%s_total", field),
				Help: fmt.Sprintf("Total %s for encoder", field),
			},
			[]string{"index", "device"},
		)
		prometheusCounters.ScalerCounters[field] = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: fmt.Sprintf("netint_scaler_%s_total", field),
				Help: fmt.Sprintf("Total %s for scaler", field),
			},
			[]string{"index", "device"},
		)
		prometheusCounters.AICounters[field] = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
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
		log.Error().Err(err).Msgf("Error parsing output:")
		return
	}
	updateTemperatureForDevices(&devices.Decoders)
	updateTemperatureForDevices(&devices.Encoders)
	updateTemperatureForDevices(&devices.Scalers)
	updateTemperatureForDevices(&devices.AIs)
	updateMetrics(devices.Decoders, prometheusCounters.DecoderCounters)
	updateMetrics(devices.Encoders, prometheusCounters.EncoderCounters)
	updateMetrics(devices.Scalers, prometheusCounters.ScalerCounters)
	updateMetrics(devices.AIs, prometheusCounters.AICounters)
	load1, load5, load15, err := readSysload()
	if err != nil {
		log.Error().Err(err).Msg("Error leyendo carga del sistema")
	} else {
		sysloadGauge.WithLabelValues("1m").Set(load1)
		sysloadGauge.WithLabelValues("5m").Set(load5)
		sysloadGauge.WithLabelValues("15m").Set(load15)
	}
	log.Debug().Msg("Metrics updated")
}

func updateMetrics(components []Metadata, counters map[string]*prometheus.GaugeVec) {
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
			counters[field].With(labels).Set(float64(value))
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
				if len(fields) < 8 {
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
				namespace := ""
				if len(fields) > 8 {
					namespace = fields[8]
				}

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
		log.Error().Err(err).Msgf("error executing nvme smart-log: %v", err)
		return 0, err
	}
	var nvmeMetadata NvmeMetadata
	err = json.Unmarshal(output, &nvmeMetadata)
	if err != nil {
		log.Error().Err(err).Msgf("error unmarshaling nvme smart-log output: %v", err)
		return 0, err
	}

	return nvmeMetadata.Temperature, nil
}

func readSysload() (float64, float64, float64, error) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, 0, 0, err
	}

	parts := strings.Fields(string(data))
	if len(parts) < 3 {
		log.Error().Err(err).Msgf("invalid format in /proc/loadavg")
		return 0, 0, 0, err
	}

	load1, err1 := strconv.ParseFloat(parts[0], 64)
	load5, err2 := strconv.ParseFloat(parts[1], 64)
	load15, err3 := strconv.ParseFloat(parts[2], 64)

	if err1 != nil || err2 != nil || err3 != nil {
		log.Error().Err(err).Msgf("error parsing values")
		return 0, 0, 0, err
	}

	return load1, load5, load15, nil
}

func extractVPUValue(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "VPU=") {
			return strings.TrimPrefix(line, "VPU="), nil
		}
	}
	return "", fmt.Errorf("VPU not found in %s", filePath)
}
