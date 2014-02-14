package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"

	"./https_everywhere"
)

var (
	rulesetLock = new(sync.RWMutex)
	ruleset     https_everywhere.RuleSet

	outputChannel    = make(chan string)
	errOutputChannel = make(chan string)
	workerWG         = new(sync.WaitGroup)
)

var (
	ruleDirectory = flag.String("rule_directory", "rules", "The directory which we search for rules to use. This directory must contain XML files of the correct format.")
	maxProcs      = flag.Int("max_procs", runtime.NumCPU(), "The maximum number of concurrent processes to use for URL parsing")
)

type MessageInfo struct {
	RequestID string
	URL       string
	IP        string
	Method    string
}

func handleIncomingMessage(messageInfo *MessageInfo) {
	defer workerWG.Done()
	defer func() {
		if err := recover(); err != nil {
			outputChannel <- fmt.Sprintf("%s BH message=%s\n", messageInfo.RequestID, err)
		}
	}()

	rulesetLock.RLock()
	defer rulesetLock.RUnlock()
	applied, newUrl, err := ruleset.Apply(messageInfo.URL, "")
	if err != nil {
		panic(err)
	}
	if !applied {
		outputChannel <- messageInfo.RequestID + " OK\n"
		return
	}
	outputChannel <- fmt.Sprintf("%s OK status=301 url=\"%s\"\n", messageInfo.RequestID, newUrl)
}

func main() {
	flag.Parse()

	runtime.GOMAXPROCS(*maxProcs)

	var err error
	ruleset, err = https_everywhere.ParseDirectory(*ruleDirectory)
	if err != nil {
		panic(err)
	}

	outputWG := new(sync.WaitGroup)
	outputWG.Add(1)
	go func() {
		defer outputWG.Done()
		for output := range outputChannel {
			io.WriteString(os.Stdout, output)
		}
	}()
	outputWG.Add(1)
	go func() {
		defer outputWG.Done()
		for output := range errOutputChannel {
			io.WriteString(os.Stderr, output)
		}
	}()

	inReader := bufio.NewReader(os.Stdin)
	for {
		incomingLine, err := inReader.ReadString('\n')
		if err == io.EOF {
			break
		} else if err != nil {
			panic(err)
		}

		// RequestID URL IP/FQDN IDENT Method kv-pair
		splitLine := strings.Split(incomingLine, " ")
		if len(splitLine) < 6 {
			continue
		}
		ipSlit := strings.Split(splitLine[2], "/")
		workerWG.Add(1)
		go handleIncomingMessage(&MessageInfo{
			RequestID: splitLine[0],
			URL:       splitLine[1],
			IP:        ipSlit[0],
			Method:    splitLine[4],
		})
	}

	workerWG.Wait()
	close(outputChannel)
	outputWG.Wait()
}
