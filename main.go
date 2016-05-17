package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/chzyer/readline"
)

const (
	promptChar = "➜ "
)

var (
	fHeaders         = flag.Bool("h", false, "print headers after the response")
	fReplay          = flag.String("replay", "", "replay")
	fContinueOnError = flag.Bool("c", false, "continue on error")

	client = New("")

	completer = readline.NewPrefixCompleter(
		readline.PcItem("GET"),
		readline.PcItem("PUT"),
		readline.PcItem("POST"),
		readline.PcItem("DELETE"),
		readline.PcItem("HEAD"),
		readline.PcItem("PATCH"),
		readline.PcItem("reset"),
		readline.PcItem("clear"),
		readline.PcItem("exit"),
		readline.PcItem("quit"),
		readline.PcItem("get",
			readline.PcItem("url"),
		),
		readline.PcItem("set",
			readline.PcItem("url"),
		),
		readline.PcItem("help"),
	)
)

func init() {
	flag.Parse()
	log.SetFlags(0)
	log.SetPrefix(" ")
	if flag.NArg() > 0 {
		client.BasaeURL = flag.Arg(0)
	}
}

func main() {
	if *fReplay != "" {
		replayFile(*fReplay)
		return
	}
	rl, err := readline.NewEx(&readline.Config{
		Prompt:       promptChar,
		AutoComplete: completer,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer rl.Close()
	log.SetOutput(rl.Stderr())

	for {
		line, err := rl.Readline()
		if err != nil { // io.EOF, readline.ErrInterrupt
			break
		}
		args := safeSplit(line)
		if len(args) == 0 {
			goto invalidCommand
		}
		switch args[0] {
		case "set":
			if len(args) != 3 || args[1] != "url" {
				goto invalidCommand
			}
			client.BasaeURL = args[2]
		case "get":
			if len(args) != 2 {
				goto invalidCommand
			}
			log.Println("BaseURL:", client.BasaeURL)
		case "reset":
			client.Reset()
			rl.SetPrompt(promptChar)
		case "clear":
			rl.Clean()
			rl.SetPrompt(promptChar)
		case "DEL":
			args[0] = "DELETE"
			fallthrough
		case "GET", "POST", "PUT", "DELETE", "HEAD", "PATCH":
			if len(args) < 2 || len(args) > 3 {
				goto invalidCommand
			}
			doReq(rl, args)
		case "exit", "quit", "q":
			return
		default:
			goto invalidCommand
		}
		continue

	invalidCommand:
		log.Printf("invalid args: %q", args)
		rl.SetPrompt("[err] " + promptChar)
	}
}

func doReq(rl *readline.Instance, args []string) {
	var body io.Reader
	method, path := args[0], args[1]
	if len(args) == 3 {
		body = strings.NewReader(args[2])
	}
	r := client.Do(method, path, body, nil)
	if r.Err != nil {
		rl.SetPrompt("[err] " + promptChar)
		log.Println(r.Err)
		return
	}
	rl.SetPrompt(fmt.Sprintf("[%d] %s", r.Status, promptChar))
	fmt.Printf("Response: %s\n", bytes.TrimSpace(r.Value))
	if *fHeaders {
		fmt.Printf("Headers: %q\n", r.Header)
	}
}

// from https://gist.github.com/jmervine/d88c75329f98e09f5c87
func safeSplit(s string) (res []string) {
	var (
		inquote, block string

		addRes = func(s string) {
			if s = strings.TrimSpace(strings.Trim(s, inquote)); s != "" {
				res = append(res, s)
			}
			inquote, block = "", ""
		}
		split = strings.Split(s, " ")
	)

	for _, i := range split {
		if inquote == "" {
			if strings.HasPrefix(i, "`") || strings.HasPrefix(i, "'") || strings.HasPrefix(i, `"`) {
				inquote = string(i[0])
				if strings.HasSuffix(i, inquote) {
					addRes(i)
					continue
				}
				block = strings.TrimPrefix(i, inquote) + " "
			} else {
				addRes(i)
			}
		} else {
			if !strings.HasSuffix(i, inquote) {
				block += i + " "
			} else {
				addRes(block + i)
			}
		}
	}
	if inquote != "" {
		log.Printf("mismatched quotes")
		return nil
	}
	return
}

func replayFile(fn string) {
	if fn == "-" || fn == "/dev/stdin" {
		replay(os.Stdin)
		return
	}
	f, err := os.Open(fn)
	if err != nil {
		log.Fatal(err)
	}
	replay(f)
	f.Close()
}

func replay(in io.Reader) {
	lf := log.Flags()
	defer log.SetFlags(lf)
	log.SetFlags(log.Lshortfile)

	br := bufio.NewScanner(in)
	var cmdArgs []string
	var exp bool
	for br.Scan() {
		args := safeSplit(br.Text())
		if len(args) == 0 {
			continue
		}
		//log.Println(args)
		if !exp {
			if cmdArgs = args; len(cmdArgs) == 0 || strings.HasPrefix(cmdArgs[0], "//") {
				cmdArgs = nil
			} else if cmdArgs[0] == "set" && cmdArgs[1] == "url" {
				client.BasaeURL = cmdArgs[2]
				cmdArgs = nil
			} else {
				exp = true
			}
			continue
		}
		exp = false
		if len(args) < 2 && len(args) > 3 {
			log.Fatalf("expected line format: status-code `json-response`, got: %q", args)
		}
		var body io.Reader
		if len(cmdArgs) == 3 {
			body = strings.NewReader(cmdArgs[2])
		}
		r := client.Do(cmdArgs[0], cmdArgs[1], body, nil)
		if r.Err != nil {
			log.Printf("%s %s: %v", cmdArgs[0], cmdArgs[1], r.Err)
			if !*fContinueOnError {
				return
			}
			continue
		}
		if args[0] != strconv.Itoa(r.Status) {
			log.Printf("%s %s: wanted %s, got %d: %s", cmdArgs[0], cmdArgs[1], args[0], r.Status, r.Value)
			if !*fContinueOnError {
				return
			}
			continue
		}
		if err := compareRes([]byte(args[1]), r.Value); err != nil {
			log.Printf("%s %s: %v", cmdArgs[0], cmdArgs[1], err)
			if !*fContinueOnError {
				return
			}
			continue
		}
		log.Printf("> %s %s: %d %s", cmdArgs[0], cmdArgs[1], r.Status, r.Value)
	}
}

func compareRes(a, b []byte) error {
	var am, bm map[string]interface{}
	if err := json.Unmarshal(a, &am); err != nil {
		return fmt.Errorf("%s: %v", a, err)
	}
	if err := json.Unmarshal(b, &bm); err != nil {
		return fmt.Errorf("%s: %v", b, err)
	}
	if !reflect.DeepEqual(am, bm) {
		return fmt.Errorf("%s != %s", a, b)
	}
	return nil
}
