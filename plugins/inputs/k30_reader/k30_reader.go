package k30_reader

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
	"math"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// default values
const (
	GATTTOOL  = "/usr/bin/gatttool"
	MACADDR   = "C1:C4:E4:05:14:95"
	VARHANDLE = "0x000e"
	GATTFLAGS = "-t random --char-read"
)

// env variable names
const (
	GATT_TOOL  = "GATTTOOL"
	MAC_ADDR   = "MACADDR"
	VAR_HANDLE = "VAR_HANDLE"
	GATT_FLAGS = "GATTFLAGS"
)

type Wireless struct {
	ProcNetWireless string `toml:"proc_net_wireless"`
	DumpZeros       bool   `toml:"dump_zeros"`
}

var sampleConfig = `
  ## command  for reading. If empty default path will be used:
  ##    gatttool -b C1:C4:E4:05:14:95 -t random --char-read  --handle=0x000e
  ## This can also be overridden with env variable, see README.
  gatttool = "/usr/bin/gatttool"
  macaddr = "C1:C4:E4:05:14:95"
  varhandle = "0x000e"
  gattflags = "-t random --char-read"
  ## dump metrics with 0 values too
  dump_zeros       = true
`

type K30 struct {
	CMD       string `toml:"gatttool"`
	ADDR      string `toml:"macaddr"`
	HANDLE    string `toml:"varhandle"`
	FLAGS     string `toml:"gattflags"`
	DumpZeros bool   `toml:"dump_zeros"`
}

var (
	colonByte = []byte(":")
	spaceByte = []byte(" ")
	emptyByte = []byte("")
)

func Float32frombytes(bytes []byte) float32 {
	bits := binary.LittleEndian.Uint32(bytes)
	float := math.Float32frombits(bits)
	return float
}

func Float32bytes(float float32) []byte {
	bits := math.Float32bits(float)
	bytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(bytes, bits)
	return bytes
}
func (ns *K30) Description() string {
	return "Collect CO2 Readings via Bluetooth from a K30-enabled Arduino"
}

func (ns *K30) SampleConfig() string {
	return sampleConfig
}
func exe_cmd(cmd string, wg *sync.WaitGroup) ([]byte, error) {
	parts := strings.Fields(cmd)
	head := parts[0]
	parts = parts[1:len(parts)]
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel() // The cancel should be deferred so resources are cleaned up

	// Create the command with our context
	out, err := exec.CommandContext(ctx, head, parts...).Output()

	// We want to check the context error to see if the timeout was executed.
	// The error returned by cmd.Output() will be OS specific based on what
	// happens when a process is killed.
	if ctx.Err() == context.DeadlineExceeded {
		wg.Done()
		return nil, err
	}
	wg.Done() // Need to signal to waitgroup that this goroutine is done
	return out, err
}

func (ns *K30) Gather(acc telegraf.Accumulator) error {
	ns.loadPath()
	wg := new(sync.WaitGroup)
	wg.Add(1)
	built_cmd := ns.CMD + " -b " + ns.ADDR + " " + ns.FLAGS + " --handle=" + ns.HANDLE
	k30, err := exe_cmd(built_cmd, wg)
	if err != nil {
		return err
	}
	err = ns.gatherk30(k30, acc)
	if err != nil {
		return err
	}
	return nil
}

func (ns *K30) gatherk30(data []byte, acc telegraf.Accumulator) error {
	tags := map[string]string{}
	metrics := map[string]interface{}{}
	tags["sensor"] = "k30_co2"
	result := bytes.Split(data, colonByte)
	fd := bytes.Fields(result[1])
	reading := make([]byte, 4)
	for x := 0; x < len(fd); x++ {
		data, err := hex.DecodeString(string(fd[x]))
		if err != nil {
			panic(err)
		}
		reading[x] = data[0]
	}
	float := Float32frombytes(reading)
	metrics["co2"] = float
	acc.AddFields("k30_reader", metrics, tags)
	return nil
}

// loadPath can be used to read path firstly from config
// if it is empty then try read from env variables
func (ns *K30) loadPath() {
	if ns.CMD == "" {
		ns.CMD = proc(GATT_TOOL, "")
	}
	if ns.ADDR == "" {
		ns.ADDR = proc(MAC_ADDR, "")
	}
	if ns.HANDLE == "" {
		ns.HANDLE = proc(VAR_HANDLE, "")
	}
	if ns.FLAGS == "" {
		ns.FLAGS = proc(GATT_FLAGS, "")
	}
}

// proc can be used to read file paths from env
func proc(env, path string) string {
	// try to read full file path
	if p := os.Getenv(env); p != "" {
		return p
	}
	return env
}

func init() {
	// this only works on Mac OS X, so if we're not running on Mac, punt.
	if runtime.GOOS != "linux" {
		return
	}
	inputs.Add("k30_reader", func() telegraf.Input {
		return &K30{}
	})
}
