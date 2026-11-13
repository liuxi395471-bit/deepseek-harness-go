// Package main 是一个联调冒烟脚本：对 dsh serve 的 HTTP/SSE 接口做端到端校验。
//
// 用法：go run ./tools/smoke -base http://127.0.0.1:8080 -token test-token-123
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

var (
	base  = flag.String("base", "http://127.0.0.1:8080", "base url")
	token = flag.String("token", "test-token-123", "auth token")
)

var (
	pass int
	fail int
)

func ok(name string) {
	pass++
	fmt.Printf("  [PASS] %s\n", name)
}

func bad(name string, detail string) {
	fail++
	fmt.Printf("  [FAIL] %s :: %s\n", name, detail)
}

func check(cond bool, name string, detail string) {
	if cond {
		ok(name)
	} else {
		bad(name, detail)
	}
}

func authed(method, path string, body []byte) (*http.Response, []byte) {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, *base+path, rdr)
	if err != nil {
		return nil, nil
	}
	if *token != "" {
		req.Header.Set("Authorization", "Bearer "+*token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

func main() {
	fmt.Println("=== dsh serve integration smoke ===")
	fmt.Printf("base=%s\n\n", *base)

	// ---------- 1. healthz ----------
	fmt.Println("[1] healthz / auth")
	r, b := authed("GET", "/healthz", nil)
	check(r != nil && r.StatusCode == 200, "GET /healthz -> 200",
		fmt.Sprintf("resp=%v", r))
	var hz map[string]any
	json.Unmarshal(b, &hz)
	check(hz["ok"] == true, "healthz body has ok=true", string(b))

	// unauthenticated API must be rejected
	r2, b2 := authed("GET", "/api/sessions", nil)
	r2b, _ := http.NewRequest("GET", *base+"/api/sessions", nil)
	resp2, err := http.DefaultClient.Do(r2b)
	if err == nil {
		defer resp2.Body.Close()
		io.ReadAll(resp2.Body)
		check(resp2.StatusCode == 401, "GET /api/sessions without token -> 401",
			fmt.Sprintf("got %d", resp2.StatusCode))
	} else {
		bad("GET /api/sessions without token -> 401", err.Error())
	}
	_ = r2
	_ = b2

	// ---------- 2. non-stream message ----------
	fmt.Println("\n[2] POST /api/agent/message")
	reqBody, _ := json.Marshal(map[string]any{"prompt": "你好"})
	r, b = authed("POST", "/api/agent/message", reqBody)
	if r == nil {
		bad("POST /api/agent/message", "no response")
	} else {
		check(r.StatusCode == 200, "POST /api/agent/message -> 200",
			fmt.Sprintf("got %d body=%s", r.StatusCode, string(b)))
		var mr map[string]any
		if err := json.Unmarshal(b, &mr); err == nil {
			check(mr["session_id"] != nil && mr["session_id"] != "", "message resp has session_id", string(b))
			check(mr["messages"] != nil, "message resp has messages", string(b))
			check(mr["usage"] != nil, "message resp has usage", string(b))
			check(mr["stop_reason"] != nil, "message resp has stop_reason", string(b))
			check(mr["rounds"] != nil, "message resp has rounds", string(b))
			usg, _ := mr["usage"].(map[string]any)
			check(usg != nil && usg["prompt_tokens"] != nil && usg["completion_tokens"] != nil && usg["total_tokens"] != nil,
				"usage has prompt/completion/total tokens", string(b))
		} else {
			bad("message resp is valid JSON", string(b))
		}
		sessID, _ := mr["session_id"].(string)
		fmt.Printf("        session_id=%s\n", sessID)

		// ---------- 3. session detail ----------
		fmt.Println("\n[3] GET /api/sessions/{id}")
		rd, bd := authed("GET", "/api/sessions/"+sessID, nil)
		check(rd != nil && rd.StatusCode == 200, "GET session detail -> 200",
			fmt.Sprintf("got %v", rd))
		var sd map[string]any
		if err := json.Unmarshal(bd, &sd); err == nil {
			for _, k := range []string{"id", "created_at", "updated_at", "rounds", "preview", "messages", "usage"} {
				check(sd[k] != nil, "session detail has key '"+k+"'", string(bd))
			}
			check(sd["id"] == sessID, "session detail id matches", string(bd))
			// updated_at 是 RFC3339 字符串（后端 store 用毫秒存、这里格式化为 UTC）。
			tsStr, _ := sd["updated_at"].(string)
			ts, perr := time.Parse(time.RFC3339, tsStr)
			check(perr == nil && time.Since(ts) < 10*time.Minute && ts.After(time.Now().Add(-10*time.Minute)),
				"updated_at is a recent RFC3339 timestamp",
				fmt.Sprintf("updated_at=%q err=%v", tsStr, perr))
		} else {
			bad("session detail is valid JSON", string(bd))
		}

		// ---------- 4. session list ----------
		fmt.Println("\n[4] GET /api/sessions")
		rl, bl := authed("GET", "/api/sessions", nil)
		check(rl != nil && rl.StatusCode == 200, "GET /api/sessions -> 200", fmt.Sprintf("got %v", rl))
		var lst any
		if json.Unmarshal(bl, &lst) == nil {
			check(lst != nil, "sessions list is valid JSON", string(bl))
		} else {
			bad("sessions list is valid JSON", string(bl))
		}
		if strings.Contains(string(bl), sessID) {
			ok("sessions list contains the new session")
		} else {
			bad("sessions list contains the new session", string(bl))
		}
	}

	// ---------- 5. SSE stream ----------
	fmt.Println("\n[5] GET /api/agent/stream (SSE)")
	sreq, _ := http.NewRequest("GET", *base+"/api/agent/stream?prompt="+url.QueryEscape("流式测试"), nil)
	sreq.Header.Set("Authorization", "Bearer "+*token)
	sreq.Header.Set("Accept", "text/event-stream")
	sresp, err := http.DefaultClient.Do(sreq)
	if err != nil {
		bad("open SSE stream", err.Error())
	} else {
		check(sresp.StatusCode == 200, "SSE -> 200", fmt.Sprintf("got %d", sresp.StatusCode))
		ct := sresp.Header.Get("Content-Type")
		check(strings.Contains(ct, "text/event-stream"), "SSE Content-Type is text/event-stream", ct)

		events := map[string]int{}
		var order []string
		var payloadText strings.Builder
		rd := bufio.NewReader(sresp.Body)
		var evName string
		done := false
		deadline := time.Now().Add(30 * time.Second)
		for !done && time.Now().Before(deadline) {
			line, err := rd.ReadString('\n')
			if err != nil {
				break
			}
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				// 空行 = 一帧结束，此时才结算事件名。
				if evName != "" {
					events[evName]++
					order = append(order, evName)
					if evName == "loop_done" {
						done = true
					}
				}
				evName = ""
				continue
			}
			if strings.HasPrefix(line, "event:") {
				evName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			} else if strings.HasPrefix(line, "data:") {
				payloadText.WriteString(strings.TrimPrefix(line, "data:"))
				payloadText.WriteString("\n")
			}
		}
		sresp.Body.Close()

		fmt.Printf("        event order: %v\n", order)
		for _, e := range []string{"assistant_delta", "loop_done"} {
			if events[e] > 0 {
				ok("SSE emits '" + e + "'")
			} else {
				bad("SSE emits '"+e+"'", fmt.Sprintf("events=%v", events))
			}
		}
		if strings.Contains(payloadText.String(), "流式") {
			ok("SSE payload contains streamed text")
		} else {
			bad("SSE payload contains streamed text", payloadText.String()[:min(300, len(payloadText.String()))])
		}
		if strings.Contains(payloadText.String(), "session_id") {
			ok("SSE loop_done contains session_id")
		} else {
			bad("SSE loop_done contains session_id", payloadText.String()[:min(500, len(payloadText.String()))])
		}
		if strings.Contains(payloadText.String(), "usage") {
			ok("SSE loop_done contains usage")
		} else {
			bad("SSE loop_done contains usage", payloadText.String()[:min(500, len(payloadText.String()))])
		}
	}

	// ---------- 6. tool call ----------
	fmt.Println("\n[6] tool call round-trip")
	tbody, _ := json.Marshal(map[string]any{"prompt": "TOOL: greet 前端联调"})
	rt, bt := authed("POST", "/api/agent/message", tbody)
	if rt == nil {
		bad("POST tool-call message", "no response")
	} else {
		check(rt.StatusCode == 200, "POST tool-call message -> 200",
			fmt.Sprintf("got %d body=%s", rt.StatusCode, string(bt)))
		var tm map[string]any
		if json.Unmarshal(bt, &tm) == nil {
			msgs, _ := tm["messages"].([]any)
			foundTool := false
			foundToolResult := false
			for _, mm := range msgs {
				m, _ := mm.(map[string]any)
				if m == nil {
					continue
				}
				role, _ := m["role"].(string)
				if role == "tool" {
					foundToolResult = true
				}
				if tcs, ok := m["tool_calls"].([]any); ok && len(tcs) > 0 {
					foundTool = true
				}
			}
			check(foundTool, "assistant message carries tool_calls", string(bt)[:min(600, len(bt))])
			check(foundToolResult, "conversation includes role=tool result", string(bt)[:min(600, len(bt))])
			rounds, _ := tm["rounds"].(float64)
			check(rounds >= 2, "rounds >= 2 for a tool call (got "+fmt.Sprint(rounds)+")", string(bt))
		} else {
			bad("tool-call resp is valid JSON", string(bt))
		}
	}

	// ---------- 7. concurrency guard ----------
	fmt.Println("\n[7] concurrency guard (same session -> 409)")
	sessBody, _ := json.Marshal(map[string]any{"prompt": "并发测试", "session_id": "conc-test-001"})
	// 必须真正并发：串行发起时第一个请求已经跑完并 release 了 session，
	// 第二个会正常成功，测不出 409。
	var wg sync.WaitGroup
	codes := make([]int, 2)
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start // 同时放行，保证两个请求重叠
			resp, _ := authed("POST", "/api/agent/message", sessBody)
			if resp != nil {
				codes[idx] = resp.StatusCode
			}
		}(i)
	}
	close(start)
	wg.Wait()
	check(codes[0] != 0 && codes[1] != 0, "both concurrent requests returned",
		fmt.Sprintf("codes=%v", codes))
	has409 := codes[0] == 409 || codes[1] == 409
	check(has409, "one of the concurrent requests gets 409", fmt.Sprintf("codes=%v", codes))

	// ---------- 8. unknown session -> error ----------
	fmt.Println("\n[8] unknown session id")
	ubody, _ := json.Marshal(map[string]any{"prompt": "x", "session_id": "does-not-exist-xyz"})
	ru, _ := authed("POST", "/api/agent/message", ubody)
	if ru != nil {
		check(ru.StatusCode >= 400, "unknown session -> 4xx/5xx", fmt.Sprintf("got %d", ru.StatusCode))
	} else {
		bad("unknown session -> error", "no response")
	}

	// ---------- 9. SPA static assets ----------
	fmt.Println("\n[9] SPA static assets")
	ireq, _ := http.NewRequest("GET", *base+"/", nil)
	iresp, err := http.DefaultClient.Do(ireq)
	if err == nil {
		defer iresp.Body.Close()
		ib, _ := io.ReadAll(iresp.Body)
		check(iresp.StatusCode == 200, "GET / -> 200", fmt.Sprintf("got %d", iresp.StatusCode))
		check(strings.Contains(string(ib), "<div id=\"app\">"), "index.html mounts the Vue app",
			string(ib)[:min(300, len(ib))])
		check(strings.Contains(string(ib), "/assets/index-"), "index.html references hashed assets",
			string(ib)[:min(300, len(ib))])
	} else {
		bad("GET /", err.Error())
	}

	fmt.Printf("\n=== summary: %d passed, %d failed ===\n", pass, fail)
	if fail > 0 {
		os.Exit(1)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
