package main

// import "fmt"

// type Tier string

// const (
// 	TIER_FREE       Tier = "FREE"
// 	TIER_PRO        Tier = "PRO"
// 	TIER_ENTERPRISE Tier = "ENTERPRISE"
// )

// type RateLimiter struct {
// 	Rules                  map[string]*Rule
// 	Clients                map[string]*Client
// 	Q                      map[Tier]*Quota
// 	ConsecutiveBlocksLimit int
// 	BanDurationMs          int64
// }

// type Rule struct {
// 	ClientID       string
// 	MaxRequests    int
// 	WindowSizeMs   int64
// 	ConsumedUnits  int
// 	Blocks         int
// 	BannedUntil    int64
// 	RequestHistory map[int64][]int64
// 	SlidingWindow  []int64
// 	Costs          []int
// }

// type Client struct {
// 	ClientID    string
// 	ClientLevel Tier
// }

// type Quota struct {
// 	Level        Tier
// 	MaxUnits     int
// 	windowSizeMs int64
// }

// func NewRateLimiter() *RateLimiter {
// 	return &RateLimiter{
// 		Rules:   make(map[string]*Rule),
// 		Clients: make(map[string]*Client),
// 		Q: map[Tier]*Quota{
// 			TIER_FREE:       NewQuota(TIER_FREE),
// 			TIER_PRO:        NewQuota(TIER_PRO),
// 			TIER_ENTERPRISE: NewQuota(TIER_ENTERPRISE),
// 		},
// 	}
// }

// func NewRule(client string, maxReqs int, windowSizeMs int64) *Rule {
// 	return &Rule{
// 		ClientID:       client,
// 		MaxRequests:    maxReqs,
// 		WindowSizeMs:   windowSizeMs,
// 		RequestHistory: make(map[int64][]int64),
// 	}
// }

// func NewClient(client string, tier Tier) *Client {
// 	return &Client{
// 		ClientID:    client,
// 		ClientLevel: tier,
// 	}
// }

// func NewQuota(level Tier) *Quota {
// 	return &Quota{Level: level}
// }

// func (rule *Rule) getTargetWindowIdx(timestamp int64) int64 {
// 	window := rule.WindowSizeMs
// 	idx := timestamp / window
// 	return idx
// }

// func (rule *Rule) isSliding() bool {
// 	return rule.SlidingWindow != nil
// }

// func (rule *Rule) dropExceededReqs(timestamp int64) int {
// 	startTime := timestamp - rule.WindowSizeMs + 1
// 	idx := len(rule.SlidingWindow)
// 	for i := 0; i < len(rule.SlidingWindow); i++ {
// 		if rule.SlidingWindow[i] >= startTime {
// 			idx = i
// 			break
// 		}
// 	}
// 	rule.SlidingWindow = rule.SlidingWindow[idx:]
// 	return idx
// }

// func (rule *Rule) handleRequestFailed() {
// 	rule.Blocks++
// }

// func (rule *Rule) isDuringBanned(timestamp int64) bool {
// 	return rule.BannedUntil > timestamp
// }

// func (rule *Rule) resetBlocks() {
// 	rule.Blocks = 0
// }

// func (rule *Rule) handleBanUser(bannedUntil int64, blockLimit int) {
// 	if rule.Blocks >= blockLimit {
// 		rule.BannedUntil = bannedUntil
// 		rule.Blocks = 0
// 	}
// }

// func (rule *Rule) dropExceddedCosts(index int) {
// 	rule.Costs = rule.Costs[index:]
// 	consumerUnits := 0
// 	for _, c := range rule.Costs {
// 		consumerUnits += c
// 	}
// 	rule.ConsumedUnits = consumerUnits
// }

// func (rule *Rule) updateTargetWindow(index, timestamp int64) {
// 	rule.RequestHistory[index] = append(rule.RequestHistory[index], timestamp)
// }

// func (rule *Rule) updateSlidingWindow(timestamp int64) {
// 	rule.SlidingWindow = append(rule.SlidingWindow, timestamp)
// }

// func (rule *Rule) updateCost(cost int) {
// 	rule.Costs = append(rule.Costs, cost)
// }

// func (r *RateLimiter) SetLimit(clientID string, maxRequests int, windowSizeMs int64) bool {
// 	if clientID == "" || maxRequests <= 0 || windowSizeMs <= 0 {
// 		return false
// 	}
// 	rule := NewRule(clientID, maxRequests, windowSizeMs)
// 	r.Rules[clientID] = rule
// 	return true
// }

// func (r *RateLimiter) AllowRequest(clientID string, timestamp int64) bool {
// 	rule, ok := r.Rules[clientID]
// 	if !ok {
// 		return true
// 	}

// 	if rule.isDuringBanned(timestamp) {
// 		return false
// 	}

// 	if _, ok := r.Clients[clientID]; ok {
// 		return r.AllowRequestWithCost(clientID, timestamp, 1)
// 	} else {
// 		if rule.isSliding() {
// 			rule.dropExceededReqs(timestamp)
// 			if len(rule.SlidingWindow) < rule.MaxRequests {
// 				rule.updateSlidingWindow(timestamp)
// 				rule.resetBlocks()
// 				return true
// 			}

// 			if r.isAbuseRuleOn() {
// 				rule.handleRequestFailed()
// 				rule.handleBanUser(timestamp+r.BanDurationMs, r.ConsecutiveBlocksLimit)
// 			}

// 			return false
// 		} else {
// 			windowIdx := rule.getTargetWindowIdx(timestamp)

// 			if len(rule.RequestHistory[windowIdx]) < rule.MaxRequests {
// 				rule.updateTargetWindow(windowIdx, timestamp)
// 				return true
// 			}

// 			if r.isAbuseRuleOn() {
// 				rule.handleRequestFailed()
// 				rule.handleBanUser(timestamp+r.BanDurationMs, r.ConsecutiveBlocksLimit)
// 			}

// 			return false
// 		}
// 	}
// }

// func (r *RateLimiter) GetRequestCount(clientID string, timestamp int64) int {
// 	rule, ok := r.Rules[clientID]
// 	if !ok {
// 		return 0
// 	}

// 	if rule.isSliding() {
// 		rule.dropExceededReqs(timestamp)
// 		return len(rule.SlidingWindow)
// 	} else {
// 		window := rule.RequestHistory[rule.getTargetWindowIdx(timestamp)]
// 		return len(window)
// 	}
// }

// func (r *RateLimiter) SetLimitWithMode(clientID string, maxRequests int, windowSizeMs int64, isSliding bool) bool {
// 	if clientID == "" || maxRequests <= 0 || windowSizeMs <= 0 {
// 		return false
// 	}

// 	rule, ok := r.Rules[clientID]
// 	if !ok {
// 		rule = NewRule(clientID, maxRequests, windowSizeMs)
// 		r.Rules[clientID] = rule
// 	} else {
// 		rule.MaxRequests = maxRequests
// 		rule.WindowSizeMs = windowSizeMs
// 	}

// 	if isSliding == false {
// 		rule.SlidingWindow = nil
// 	} else {
// 		if rule.SlidingWindow == nil {
// 			rule.SlidingWindow = make([]int64, 0)
// 		}
// 	}

// 	return true
// }

// func (r *RateLimiter) SetTierDefault(tier string, maxUnits int, windowSizeMs int64) bool {
// 	if tier == "" || maxUnits <= 0 || windowSizeMs <= 0 {
// 		return false
// 	}

// 	targetTier, ok := r.Q[Tier(tier)]
// 	if !ok {
// 		return false
// 	}
// 	targetTier.windowSizeMs = windowSizeMs
// 	targetTier.MaxUnits = maxUnits
// 	r.Q[Tier(tier)] = targetTier
// 	return true
// }

// func (r *RateLimiter) AssignClientTier(clientID string, tier string) bool {
// 	if clientID == "" || tier == "" {
// 		return false
// 	}

// 	targetTier, ok := r.Q[Tier(tier)]
// 	if !ok || targetTier.windowSizeMs == 0 || targetTier.MaxUnits == 0 {
// 		return false
// 	}

// 	client, ok := r.Clients[clientID]
// 	if !ok {
// 		client = NewClient(clientID, Tier(tier))
// 	}

// 	client.ClientLevel = Tier(tier)
// 	r.Clients[clientID] = client

// 	rule, ok := r.Rules[clientID]
// 	if !ok {
// 		rule = NewRule(clientID, targetTier.MaxUnits, targetTier.windowSizeMs)
// 	} else {
// 		rule.MaxRequests = targetTier.MaxUnits
// 		rule.WindowSizeMs = targetTier.windowSizeMs
// 	}
// 	rule.SlidingWindow = make([]int64, 0)
// 	r.Rules[clientID] = rule

// 	return true
// }

// func (r *RateLimiter) AllowRequestWithCost(clientID string, timestamp int64, cost int) bool {
// 	if clientID == "" || cost <= 0 {
// 		return false
// 	}

// 	rule, ok := r.Rules[clientID]
// 	if !ok {
// 		return true
// 	}

// 	if rule.isDuringBanned(timestamp) {
// 		return false
// 	}

// 	client, ok := r.Clients[clientID]
// 	if !ok {
// 		return true
// 	}

// 	idx := rule.dropExceededReqs(timestamp)
// 	rule.dropExceddedCosts(idx)

// 	if rule.ConsumedUnits+cost <= r.Q[client.ClientLevel].MaxUnits {
// 		rule.ConsumedUnits += cost
// 		rule.updateSlidingWindow(timestamp)
// 		rule.updateCost(cost)
// 		rule.resetBlocks()
// 	} else {
// 		if r.isAbuseRuleOn() {
// 			rule.handleRequestFailed()
// 			rule.handleBanUser(timestamp+r.BanDurationMs, r.ConsecutiveBlocksLimit)
// 		}
// 		return false
// 	}

// 	r.Rules[clientID] = rule
// 	return true
// }

// func (r *RateLimiter) GetUsedUnits(clientID string, timestamp int64) int {
// 	if clientID == "" {
// 		return 0
// 	}

// 	rule, ok := r.Rules[clientID]
// 	if !ok {
// 		return 0
// 	}

// 	idx := rule.dropExceededReqs(timestamp)
// 	rule.dropExceddedCosts(idx)
// 	return rule.ConsumedUnits
// }

// func (r *RateLimiter) ConfigureAbuseRule(consecutiveBlocks int, banDurationMs int64) bool {
// 	if consecutiveBlocks <= 0 || banDurationMs <= 0 {
// 		return false
// 	}

// 	r.BanDurationMs = banDurationMs
// 	r.ConsecutiveBlocksLimit = consecutiveBlocks
// 	return true
// }

// func (r *RateLimiter) isAbuseRuleOn() bool {
// 	return r.ConsecutiveBlocksLimit > 0 && r.BanDurationMs > 0
// }

// func (r *RateLimiter) IsBanned(clientID string, timestamp int64) bool {
// 	rule, ok := r.Rules[clientID]
// 	if !ok {
// 		return true
// 	}

// 	return rule.isDuringBanned(timestamp)
// }

// func runLevel1Tests() {
// 	rl := NewRateLimiter()

// 	// Test 1: SetLimit validation
// 	assert(rl.SetLimit("client_a", 3, 1000) == true, "Test 1.1 Failed: SetLimit client_a")
// 	assert(rl.SetLimit("", 3, 1000) == false, "Test 1.2 Failed: Empty clientID")
// 	assert(rl.SetLimit("client_a", 0, 1000) == false, "Test 1.3 Failed: maxRequests <= 0")
// 	assert(rl.SetLimit("client_a", 3, 0) == false, "Test 1.4 Failed: windowSizeMs <= 0")

// 	// Test 2: Unconfigured client should default allow
// 	assert(rl.AllowRequest("guest", 100) == true, "Test 2.1 Failed: Unconfigured client allow")
// 	assert(rl.GetRequestCount("guest", 100) == 0, "Test 2.2 Failed: Unconfigured count should be 0")

// 	// Test 3: Fixed Window allow and block
// 	// client_a: 3 requests per 1000ms window.
// 	// Window 0: [0, 999]
// 	assert(rl.AllowRequest("client_a", 100) == true, "Test 3.1: 1st req in window 0")
// 	assert(rl.AllowRequest("client_a", 200) == true, "Test 3.2: 2nd req in window 0")
// 	assert(rl.AllowRequest("client_a", 500) == true, "Test 3.3: 3rd req in window 0")
// 	assert(rl.GetRequestCount("client_a", 500) == 3, "Test 3.4: Count should be 3")

// 	// 4th request in window 0 -> reject
// 	assert(rl.AllowRequest("client_a", 800) == false, "Test 3.5: 4th req rejected")
// 	assert(rl.GetRequestCount("client_a", 800) == 3, "Test 3.6: Count stays 3 after rejection")

// 	// Test 4: Next Window (Window 1: [1000, 1999])
// 	assert(rl.AllowRequest("client_a", 1000) == true, "Test 4.1: 1st req in window 1")
// 	assert(rl.GetRequestCount("client_a", 1000) == 1, "Test 4.2: Count reset to 1 in new window")
// 	assert(rl.GetRequestCount("client_a", 500) == 3, "Test 4.3: Old window count preserved")

// 	fmt.Println(">>> Level 1 Tests Passed! <<<")
// }

// func runLevel2Tests() {
// 	rl := NewRateLimiter()

// 	// client_fixed: 固定窗口 3 reqs / 1000ms
// 	rl.SetLimit("client_fixed", 3, 1000)

// 	// client_sliding: 滑動窗口 3 reqs / 1000ms
// 	assert(rl.SetLimitWithMode("client_sliding", 3, 1000, true) == true, "Test 2.1 Failed: Set sliding limit")
// 	assert(rl.SetLimitWithMode("", 3, 1000, true) == false, "Test 2.2 Failed: Invalid clientID")

// 	// Test 1: 固定窗口在邊界處的缺陷驗證 (允許 900~1000 穿透 6 筆)
// 	assert(rl.AllowRequest("client_fixed", 900) == true, "Test 1.1")
// 	assert(rl.AllowRequest("client_fixed", 950) == true, "Test 1.2")
// 	assert(rl.AllowRequest("client_fixed", 999) == true, "Test 1.3")
// 	// 換到 Window 1: 依然可以馬上進 3 筆
// 	assert(rl.AllowRequest("client_fixed", 1000) == true, "Test 1.4")
// 	assert(rl.AllowRequest("client_fixed", 1050) == true, "Test 1.5")
// 	assert(rl.AllowRequest("client_fixed", 1099) == true, "Test 1.6")

// 	// Test 2: 滑動窗口成功擋下邊界突發 (Burst Protection)
// 	// client_sliding 打入 3 筆
// 	assert(rl.AllowRequest("client_sliding", 900) == true, "Test 2.1")
// 	assert(rl.AllowRequest("client_sliding", 950) == true, "Test 2.2")
// 	assert(rl.AllowRequest("client_sliding", 999) == true, "Test 2.3")
// 	assert(rl.GetRequestCount("client_sliding", 999) == 3, "Test 2.4: Count is 3")

// 	// 在 t=1000 時 (區間 [1, 1000]，包含 900, 950, 999 共 3 筆)，應被滑動窗口拒絕！
// 	assert(rl.AllowRequest("client_sliding", 1000) == false, "Test 2.5: Blocked by sliding window")
// 	assert(rl.GetRequestCount("client_sliding", 1000) == 3, "Test 2.6: Count stays 3")

// 	// 在 t=1899 時 (區間 [900, 1899]，依然包含 900, 950, 999)，應繼續被拒絕
// 	assert(rl.AllowRequest("client_sliding", 1899) == false, "Test 2.7: Still blocked at 1899")

// 	// 在 t=1900 時 (區間 [901, 1900]，900 已經滑出窗口！剩 950, 999 共 2 筆)，應允許通過！
// 	assert(rl.AllowRequest("client_sliding", 1900) == true, "Test 2.8: 900 rolled out, request allowed")
// 	assert(rl.GetRequestCount("client_sliding", 1900) == 3, "Test 2.9: 950, 999, 1900 total 3")

// 	// 再次請求在 1901 (區間 [902, 1901]，包含 950, 999, 1900 共 3 筆) -> 拒絕
// 	assert(rl.AllowRequest("client_sliding", 1901) == false, "Test 2.10: Blocked again")

// 	// Test 3: 時間往前跳躍 (所有舊請求全數過期)
// 	// 在 t=5000 時，區間 [4001, 5000]，以前的請求全部滑出
// 	assert(rl.GetRequestCount("client_sliding", 5000) == 0, "Test 3.1: All expired")
// 	assert(rl.AllowRequest("client_sliding", 5000) == true, "Test 3.2: First req in new sliding range")
// 	assert(rl.GetRequestCount("client_sliding", 5000) == 1, "Test 3.3: Count is 1")

// 	fmt.Println(">>> Level 2 Tests Passed! <<<")
// }

// func runLevel3Tests() {
// 	rl := NewRateLimiter()

// 	// Test 1: Tier 設定與驗證
// 	assert(rl.SetTierDefault("FREE", 10, 1000) == true, "Test 1.1: Set FREE tier (10 units / 1s)")
// 	assert(rl.SetTierDefault("PRO", 100, 1000) == true, "Test 1.2: Set PRO tier (100 units / 1s)")
// 	assert(rl.SetTierDefault("", 10, 1000) == false, "Test 1.3: Empty tier name")
// 	assert(rl.SetTierDefault("FREE", 0, 1000) == false, "Test 1.4: Invalid maxUnits")

// 	// Assign Tier
// 	assert(rl.AssignClientTier("alice", "FREE") == true, "Test 1.5: Assign alice to FREE")
// 	assert(rl.AssignClientTier("bob", "PRO") == true, "Test 1.6: Assign bob to PRO")
// 	assert(rl.AssignClientTier("charlie", "UNKNOWN_TIER") == false, "Test 1.7: Assign unknown tier fails")

// 	// Test 2: Cost-based request for Alice (FREE: 10 units / 1000ms)
// 	// Alice consumes 4 units at t=100
// 	assert(rl.AllowRequestWithCost("alice", 100, 4) == true, "Test 2.1: Alice cost 4")
// 	assert(rl.GetUsedUnits("alice", 100) == 4, "Test 2.2: Alice used 4")

// 	// Alice consumes 5 units at t=200 (Total: 9/10)
// 	assert(rl.AllowRequestWithCost("alice", 200, 5) == true, "Test 2.3: Alice cost 5")
// 	assert(rl.GetUsedUnits("alice", 200) == 9, "Test 2.4: Alice used 9")

// 	// Alice tries to consume 2 units at t=500 (9 + 2 = 11 > 10 -> Reject)
// 	assert(rl.AllowRequestWithCost("alice", 500, 2) == false, "Test 2.5: Over limit reject")
// 	assert(rl.GetUsedUnits("alice", 500) == 9, "Test 2.6: Used units stays 9")

// 	// Alice consumes 1 unit at t=600 (9 + 1 = 10 <= 10 -> Allow, hit max)
// 	assert(rl.AllowRequestWithCost("alice", 600, 1) == true, "Test 2.7: Alice cost 1 hits limit")
// 	assert(rl.GetUsedUnits("alice", 600) == 10, "Test 2.8: Alice fully saturated")

// 	// Test 3: Backward compatibility (AllowRequest implies cost=1)
// 	// At t=800, Alice has 10 units used, AllowRequest(cost 1) should be rejected
// 	assert(rl.AllowRequest("alice", 800) == false, "Test 3.1: Backward compat AllowRequest rejected")

// 	// At t=1101 (window [102, 1101]): t=100 (4 units) slides out! Current used = 5 + 1 = 6.
// 	// Capacity remaining = 10 - 6 = 4 units.
// 	assert(rl.GetUsedUnits("alice", 1101) == 6, "Test 3.2: 4 units expired, 6 units remain")
// 	assert(rl.AllowRequestWithCost("alice", 1101, 4) == true, "Test 3.3: Consumes remaining 4 units")
// 	assert(rl.GetUsedUnits("alice", 1101) == 10, "Test 3.4: Back to 10")

// 	// Test 4: Heavy cost on Bob (PRO: 100 units / 1000ms)
// 	assert(rl.AllowRequestWithCost("bob", 1000, 80) == true, "Test 4.1: Bob heavy query 80 units")
// 	assert(rl.AllowRequestWithCost("bob", 1000, 30) == false, "Test 4.2: 80+30 > 100 reject")

// 	// Test 5: Invalid cost
// 	assert(rl.AllowRequestWithCost("alice", 2000, 0) == false, "Test 5.1: Cost 0 invalid")
// 	assert(rl.AllowRequestWithCost("alice", 2000, -5) == false, "Test 5.2: Negative cost invalid")

// 	// Test 6: Unconfigured client with cost
// 	assert(rl.AllowRequestWithCost("guest", 1000, 99999) == true, "Test 6.1: Guest unlimited")
// 	assert(rl.GetUsedUnits("guest", 1000) == 0, "Test 6.2: Guest used units is 0")

// 	fmt.Println(">>> Level 3 Tests Passed! <<<")
// }

// func runLevel4Tests() {
// 	rl := NewRateLimiter()

// 	// 基礎設定：alice 為 FREE (10 units / 1000ms sliding)
// 	rl.SetTierDefault("FREE", 10, 1000)
// 	rl.AssignClientTier("alice", "FREE")

// 	// Test 1: 配置 Abuse 規則
// 	assert(rl.ConfigureAbuseRule(3, 5000) == true, "Test 1.1: 3 consecutive blocks -> ban for 5000ms")
// 	assert(rl.ConfigureAbuseRule(0, 5000) == false, "Test 1.2: Invalid blocks")
// 	assert(rl.ConfigureAbuseRule(3, 0) == false, "Test 1.3: Invalid duration")

// 	// Test 2: 正常請求並耗盡配額
// 	// alice 在 t=1000 消耗 10 點 (打滿)
// 	assert(rl.AllowRequestWithCost("alice", 1000, 10) == true, "Test 2.1: Consumed 10 units")
// 	assert(rl.IsBanned("alice", 1000) == false, "Test 2.2: Not banned yet")

// 	// Test 3: 連續觸發被拒 (Consecutive Blocks)
// 	// 第 1 次被拒 (t=1100, 超額)
// 	assert(rl.AllowRequestWithCost("alice", 1100, 1) == false, "Test 3.1: Block 1")
// 	assert(rl.IsBanned("alice", 1100) == false, "Test 3.2: Not banned after 1 block")

// 	// 第 2 次被拒 (t=1200, 超額)
// 	assert(rl.AllowRequestWithCost("alice", 1200, 1) == false, "Test 3.3: Block 2")
// 	assert(rl.IsBanned("alice", 1200) == false, "Test 3.4: Not banned after 2 blocks")

// 	// 第 3 次被拒 (t=1300, 超額) -> 達到門檻！立刻觸發 Ban 直到 1300 + 5000 = 6300
// 	assert(rl.AllowRequestWithCost("alice", 1300, 1) == false, "Test 3.5: Block 3 triggers ban")
// 	assert(rl.IsBanned("alice", 1300) == true, "Test 3.6: Banned at 1300")
// 	assert(rl.IsBanned("alice", 6299) == true, "Test 3.7: Still banned at 6299")

// 	// Test 4: 封鎖期間的行為
// 	// 在 t=2000 時，原本的滑動配額已經過期釋放了，但因為處於 Ban 狀態，依然必須被拒絕！
// 	assert(rl.AllowRequestWithCost("alice", 2000, 1) == false, "Test 4.1: Blocked by ban despite quota available")

// 	// Test 5: 解鎖機制 (Unban after timestamp >= 6300)
// 	assert(rl.IsBanned("alice", 6300) == false, "Test 5.1: Ban expired at 6300")
// 	// 恢復正常，且配額已經完全過期，可正常通過
// 	assert(rl.AllowRequestWithCost("alice", 6300, 5) == true, "Test 5.2: Allowed after unban")
// 	assert(rl.GetUsedUnits("alice", 6300) == 5, "Test 5.3: Used 5 units")

// 	// Test 6: 成功請求會重置連續被拒次數
// 	// alice 在 t=6400 消耗 5 點 (打滿 10 點)
// 	assert(rl.AllowRequestWithCost("alice", 6400, 5) == true, "Test 6.1: Full capacity")
// 	// 連續被拒 2 次
// 	assert(rl.AllowRequestWithCost("alice", 6500, 1) == false, "Test 6.2: Block 1")
// 	assert(rl.AllowRequestWithCost("alice", 6600, 1) == false, "Test 6.3: Block 2")
// 	// 等待配額釋放後成功通過 1 筆 -> 連續計數必須重置為 0！
// 	// t=7301 (區間 [6302, 7301]，t=6300 的 5 點過期，剩下 6400 的 5 點)
// 	assert(rl.AllowRequestWithCost("alice", 7301, 1) == true, "Test 6.4: Success resets consecutive blocks")
// 	// 再被拒 1 次，不會觸發 ban (因為計數重新從 1 起算)
// 	assert(rl.AllowRequestWithCost("alice", 7302, 100) == false, "Test 6.5: Block 1 of new cycle")
// 	assert(rl.IsBanned("alice", 7302) == false, "Test 6.6: Not banned")

// 	fmt.Println(">>> Level 4 Tests Passed! <<<")
// }

// func assert(cond bool, msg string) {
// 	if !cond {
// 		panic(msg)
// 	}
// }

// func main() {
// 	runLevel1Tests()
// 	runLevel2Tests()
// 	runLevel3Tests()
// 	runLevel4Tests()
// }
