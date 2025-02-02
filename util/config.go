package util

import (
	"bytes"
	"crypto/md5"
	"embed"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"math/rand"
	"os"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"time"
)

const (
	ErrorExitStatus   = -1
	SuccessExitStatus = 0
	// DefaultPubSubTopic defines the default libp2p pubsub topic subscription.
	DefaultPubSubTopic = "/libp2p/pneumaex/1.0.0"
)

//go:embed conf/default.json
var defaultConfig embed.FS

var (
	DebugMode *bool
	_         = reflect.TypeOf(AgentConfig{})
	_         = reflect.TypeOf(Beacon{})
)

//CommunicationChannels contains the contact implementations
var CommunicationChannels = map[string]Contact{}

//Contact defines required functions for communicating with the server
type Contact interface {
	Communicate(agent *AgentConfig, name string) (*Connection, error)
}

type Configuration interface {
	ApplyConfig(ac map[string]interface{})
	BuildBeacon() Beacon
}

type Operation interface {
	StartInstructions(instructions []Instruction) (ret []Instruction)
	StartInstruction(instruction Instruction) bool
	EndInstruction(instruction Instruction)
	BuildExecutingHash() string
}

type AgentConfig struct {
	Name    string
	AESKey  string
	Range   string
	Contact map[string]string
	//Address string
	UDP            string
	TCP            string
	HTTP           string
	GRPC           string
	P2P            string
	Useragent      string
	Sleep          int
	KillSleep      int
	CommandJitter  int
	CommandTimeout int
	Pid            int
	Proxy          string
	Debug          bool
	Executing      map[string]Instruction
}

type Instruction struct {
	ID        string `json:"ID"`
	Executor  string `json:"Executor"`
	Operation string `json:"operation"`
	Payload   string `json:"Payload"`
	Request   string `json:"Request"`
	Response  string
	Status    int
	Pid       int
}

func BuildAgentConfig() *AgentConfig {
	var agent AgentConfig
	data, _ := defaultConfig.ReadFile("conf/default.json")
	json.Unmarshal(data, &agent)
	agent.Name = PickName(12)
	agent.Pid = os.Getpid()
	agent.Executing = make(map[string]Instruction)
	return &agent
}

func (c *AgentConfig) SetAgentConfig(ac map[string]interface{}) {
	c.Name = applyKey(c.Name, ac, "Name").(string)
	c.AESKey = applyKey(c.AESKey, ac, "AESKey").(string)
	c.Range = applyKey(c.Range, ac, "Range").(string)
	c.Useragent = applyKey(c.Useragent, ac, "Useragent").(string)
	c.Proxy = applyKey(c.Proxy, ac, "Proxy").(string)
	c.Sleep = applyKey(c.Sleep, ac, "Sleep").(int)
	c.CommandJitter = applyKey(c.Sleep, ac, "CommandJitter").(int)
	c.UDP = applyKey(c.UDP, ac, "UDP").(string)
	c.TCP = applyKey(c.TCP, ac, "TCP").(string)
	c.HTTP = applyKey(c.HTTP, ac, "HTTP").(string)
	c.GRPC = applyKey(c.GRPC, ac, "GRPC").(string)
	c.P2P = applyKey(c.P2P, ac, "P2P").(string)
	// Init the CommunicationChannels and build a map of contact:address in c.Contact
	contacts := []string{"TCP", "UDP", "HTTP", "GRPC", "P2P"}
	for _, contact := range contacts {
		if address, ok := ac[contact]; ok {
			if len(address.(string)) > 0 {
				if _, ok = CommunicationChannels[strings.ToLower(contact)]; ok {
					c.Contact[strings.ToLower(contact)] = applyKey(c.Contact, ac, contact).(string)
				}
			}
		}
	}
}

func (c *AgentConfig) StartInstruction(instruction Instruction) bool {
	if _, ex := c.Executing[instruction.ID]; ex {
		return false
	}
	c.Executing[instruction.ID] = instruction
	return true
}

func (c *AgentConfig) StartInstructions(instructions []Instruction) (ret []Instruction) {
	for _, i := range instructions {
		if c.StartInstruction(i) {
			ret = append(ret, i)
		}
	}
	return
}

func (c *AgentConfig) EndInstruction(instruction Instruction) {
	delete(c.Executing, instruction.ID)
}

func (c *AgentConfig) BuildExecutingHash() string {
	if count := len(c.Executing); count > 0 {
		ids := make([]string, count)
		for id := range c.Executing {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		h := md5.New()
		for _, s := range ids {
			io.WriteString(h, s)
		}
		return hex.EncodeToString(h.Sum(nil))
	}
	return ""
}

func (c *AgentConfig) BuildBeacon(contact string) Beacon {
	pwd, _ := os.Getwd()
	executable, _ := os.Executable()
	hostname, _ := os.Hostname()
	return Beacon{
		Name:      c.Name,
		Target:    c.Contact[contact],
		Hostname:  hostname,
		Range:     c.Range,
		Sleep:     c.Sleep,
		Pwd:       pwd,
		Location:  executable,
		Platform:  runtime.GOOS,
		Executors: DetermineExecutors(runtime.GOOS, runtime.GOARCH),
		Executing: "",
		Links:     make([]Instruction, 0),
	}
}

func (c *AgentConfig) BuildSocketBeacon(shell string) ([]byte, error) {
	magic := []byte(".p.s.\\")
	header, err := json.Marshal(map[string]string{"name": c.Name, "shell": shell})
	if err != nil {
		return nil, err
	}
	size := new(bytes.Buffer)
	if err = binary.Write(size, binary.LittleEndian, int32(len(header))); err != nil {
		return nil, err
	}
	return bytes.Join([][]byte{magic, size.Bytes(), header}, []byte{}), nil
}

func ParseArguments(args string) []string {
	var data []string
	err := json.Unmarshal([]byte(args), &data)
	if err != nil {
		log.Fatal(err)
	}
	return data
}

func applyKey(curr interface{}, ac map[string]interface{}, key string) interface{} {
	if val, ok := ac[key]; ok {
		if key == "Sleep" && reflect.TypeOf(val).Kind() == reflect.Float64 {
			return int(reflect.ValueOf(val).Float())
		}
		return val
	}
	return curr
}

// PickName returns a random alphabetic string of length chars.
func PickName(chars int) string {
	rand.Seed(time.Now().UnixNano())
	const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	b := make([]byte, chars)
	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))]
	}
	return string(b)
}

// JitterSleep
func JitterSleep(sleep int, beaconType string) {
	rand.Seed(time.Now().UnixNano())
	min := int(float64(sleep) * .90)
	max := int(float64(sleep) * 1.10)
	randomSleep := rand.Intn(max-min+1) + min
	switch beaconType {
	case "JITTER":
		DebugLogf("[%s] Sleeping %d seconds before next TTP", beaconType, randomSleep)
	default:
		DebugLogf("[%s] Next beacon going out in %d seconds", beaconType, randomSleep)
	}
	time.Sleep(time.Duration(randomSleep) * time.Second)
}
