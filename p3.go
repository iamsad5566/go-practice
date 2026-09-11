package main

import "fmt"

type RateLimiter struct {
	Rules map[string]*Rule
}

type Rule struct {
	ClientID       string
	MaxRequests    int
	WindowSizeMs   int64
	RequestHistory map[int64][]int64
}

func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		Rules: make(map[string]*Rule),
	}
}

func NewRule(client string, maxReqs int, windowSizeMs int64) *Rule {
	return &Rule{
		ClientID:       client,
		MaxRequests:    maxReqs,
		WindowSizeMs:   windowSizeMs,
		RequestHistory: make(map[int64][]int64),
	}
}

func (rule *Rule) getTargetWindowIdx(timestamp int64) int64 {
	window := rule.WindowSizeMs
	idx := timestamp / window
	return idx
}

func (rule *Rule) updateTargetWindow(index, timestamp int64) {
	rule.RequestHistory[index] = append(rule.RequestHistory[index], timestamp)
}

func (r *RateLimiter) SetLimit(clientID string, maxRequests int, windowSizeMs int64) bool {
	if clientID == "" || maxRequests <= 0 || windowSizeMs <= 0 {
		return false
	}
	rule := NewRule(clientID, maxRequests, windowSizeMs)
	r.Rules[clientID] = rule
	return true
}

func (r *RateLimiter) AllowRequest(clientID string, timestamp int64) bool {
	rule, ok := r.Rules[clientID]
	if !ok {
		return true
	}

	windowIdx := rule.getTargetWindowIdx(timestamp)

	if len(rule.RequestHistory[windowIdx]) < rule.MaxRequests {
		rule.updateTargetWindow(windowIdx, timestamp)
		return true
	}
	return false
}

func (r *RateLimiter) GetRequestCount(clientID string, timestamp int64) int {
	rule, ok := r.Rules[clientID]
	if !ok {
		return 0
	}

	window := rule.RequestHistory[rule.getTargetWindowIdx(timestamp)]
	return len(window)
}

func runLevel1Tests() {
	rl := NewRateLimiter()

	// Test 1: SetLimit validation
	assert(rl.SetLimit("client_a", 3, 1000) == true, "Test 1.1 Failed: SetLimit client_a")
	assert(rl.SetLimit("", 3, 1000) == false, "Test 1.2 Failed: Empty clientID")
	assert(rl.SetLimit("client_a", 0, 1000) == false, "Test 1.3 Failed: maxRequests <= 0")
	assert(rl.SetLimit("client_a", 3, 0) == false, "Test 1.4 Failed: windowSizeMs <= 0")

	// Test 2: Unconfigured client should default allow
	assert(rl.AllowRequest("guest", 100) == true, "Test 2.1 Failed: Unconfigured client allow")
	assert(rl.GetRequestCount("guest", 100) == 0, "Test 2.2 Failed: Unconfigured count should be 0")

	// Test 3: Fixed Window allow and block
	// client_a: 3 requests per 1000ms window.
	// Window 0: [0, 999]
	assert(rl.AllowRequest("client_a", 100) == true, "Test 3.1: 1st req in window 0")
	assert(rl.AllowRequest("client_a", 200) == true, "Test 3.2: 2nd req in window 0")
	assert(rl.AllowRequest("client_a", 500) == true, "Test 3.3: 3rd req in window 0")
	assert(rl.GetRequestCount("client_a", 500) == 3, "Test 3.4: Count should be 3")

	// 4th request in window 0 -> reject
	assert(rl.AllowRequest("client_a", 800) == false, "Test 3.5: 4th req rejected")
	assert(rl.GetRequestCount("client_a", 800) == 3, "Test 3.6: Count stays 3 after rejection")

	// Test 4: Next Window (Window 1: [1000, 1999])
	assert(rl.AllowRequest("client_a", 1000) == true, "Test 4.1: 1st req in window 1")
	assert(rl.GetRequestCount("client_a", 1000) == 1, "Test 4.2: Count reset to 1 in new window")
	assert(rl.GetRequestCount("client_a", 500) == 3, "Test 4.3: Old window count preserved")

	fmt.Println(">>> Level 1 Tests Passed! <<<")
}

func assert(cond bool, msg string) {
	if !cond {
		panic(msg)
	}
}

func main() {
	runLevel1Tests()
}
