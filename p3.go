package main

type RateLimiter struct {
	TimeWindow       int64 //ms
	DefaultLimit     int
	AbuseThreshold   int
	CoolDownDuration int64
	PenaltyOn        bool
	Users            map[string]*User
	EndPoints        map[string]*EndPoint
}

type User struct {
	ID                 string
	Limit              int
	BannedUntil        int64
	LastSuccessfulReqs []int64
	RejectedReqs       []int64
	TokenBucket
}

type EndPoint struct {
	Route              string
	Limit              int
	LastSuccessfulReqs []int64
}

type TokenBucket struct {
	Enabled             bool
	Capacity            int
	Tokens              int
	RefillRatePerSec    int
	LastRefillTimestamp int64
}

func NewUser(id string, limit int) *User {
	return &User{
		ID:                 id,
		Limit:              limit,
		BannedUntil:        0,
		LastSuccessfulReqs: make([]int64, 0, limit),
		RejectedReqs:       make([]int64, 0),
	}
}

func NewEndPoint(route string, limit int) *EndPoint {
	return &EndPoint{
		Route:              route,
		Limit:              limit,
		LastSuccessfulReqs: make([]int64, 0, limit),
	}
}

func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		TimeWindow:   1000,
		DefaultLimit: 5,
		Users:        make(map[string]*User),
		EndPoints:    make(map[string]*EndPoint),
		PenaltyOn:    false,
	}
}

func (u *User) ResetRateLimit() {
	u.LastSuccessfulReqs = make([]int64, 0, u.Limit)
}

func (u *User) updateLatestRequest(timestamp int64) {
	u.LastSuccessfulReqs = append(u.LastSuccessfulReqs, timestamp)
}

func (u *User) updateLatestRejectedReq(timestamp int64) {
	u.RejectedReqs = append(u.RejectedReqs, timestamp)
}

func (u *User) dropPreviousReq(timestamp, timewindow int64) {
	validIdx := getValidIdx(timestamp, timewindow, u.LastSuccessfulReqs)
	u.LastSuccessfulReqs = u.LastSuccessfulReqs[validIdx:]
}

func (u *User) dropPreviousRejectedReq(timestamp, timewindow int64) {
	validIdx := getValidIdx(timestamp, timewindow, u.RejectedReqs)
	u.RejectedReqs = u.RejectedReqs[validIdx:]
}

func (u *User) isBanned(timestamp int64) bool {
	if u.BannedUntil > timestamp {
		return true
	}
	return false
}

func (u *User) handleRejectedRequest(timestamp int64, r RateLimiter) {
	u.updateLatestRejectedReq(timestamp)
	u.dropPreviousRejectedReq(timestamp, r.TimeWindow)
	if len(u.RejectedReqs) >= r.AbuseThreshold {
		u.BannedUntil = timestamp + r.CoolDownDuration
		u.RejectedReqs = make([]int64, 0)
	}
}

func (e *EndPoint) updateLatestRequest(timestamp int64) {
	e.LastSuccessfulReqs = append(e.LastSuccessfulReqs, timestamp)
}

func (e *EndPoint) dropPreviousReq(timestamp, timewindow int64) {
	validIdx := getValidIdx(timestamp, timewindow, e.LastSuccessfulReqs)
	e.LastSuccessfulReqs = e.LastSuccessfulReqs[validIdx:]
}

func (r *RateLimiter) SetLimit(userId string, limit int) {
	if limit <= 0 {
		return
	}

	if val, ok := r.Users[userId]; ok {
		val.Limit = limit
	} else {
		r.Users[userId] = NewUser(userId, limit)
	}
}

func (r *RateLimiter) QualifiedReq(timestamp int64, userId string) bool {
	user := r.Users[userId]

	if user == nil {
		return true
	}

	validIdx := getValidIdx(timestamp, r.TimeWindow, user.LastSuccessfulReqs)

	if len(user.LastSuccessfulReqs)-validIdx < user.Limit {
		return true
	}

	return false
}

func (r *RateLimiter) AllowRequest(timestamp int64, userId string) bool {
	user := r.Users[userId]
	if user == nil {
		user = NewUser(userId, r.DefaultLimit)
		r.Users[userId] = user
	}

	if r.PenaltyOn && user.isBanned(timestamp) {
		return false
	}

	passed := r.QualifiedReq(timestamp, userId)

	if passed {
		user.dropPreviousReq(timestamp, r.TimeWindow)
		user.updateLatestRequest(timestamp)
		return true
	} else {
		user.handleRejectedRequest(timestamp, *r)
		return false
	}
}

func getValidIdx(timestamp, timeWindow int64, reqs []int64) int {
	cutOff := timestamp - timeWindow
	validIdx := len(reqs)
	for i, t := range reqs {
		if t > cutOff {
			validIdx = i
			break
		}
	}
	return validIdx
}

func (r *RateLimiter) SetRouteLimit(route string, limit int) {
	if limit <= 0 {
		return
	}

	ep, ok := r.EndPoints[route]
	if !ok {
		ep = NewEndPoint(route, limit)
		r.EndPoints[route] = ep
	} else {
		ep.Limit = limit
	}
}

func (r *RateLimiter) AllowRoutedRequest(timestamp int64, userId string, route string) bool {
	userLimitPassed := r.QualifiedReq(timestamp, userId)
	if !userLimitPassed {
		return false
	}

	endPoint := r.EndPoints[route]
	if endPoint == nil {
		return r.AllowRequest(timestamp, userId)
	} else {
		validIdx := getValidIdx(timestamp, r.TimeWindow, endPoint.LastSuccessfulReqs)
		if len(endPoint.LastSuccessfulReqs)-validIdx < endPoint.Limit {
			success := r.AllowRequest(timestamp, userId)
			if !success {
				return false
			}
			endPoint.dropPreviousReq(timestamp, r.TimeWindow)
			endPoint.updateLatestRequest(timestamp)
			return true
		} else {
			user := r.Users[userId]
			if user == nil {
				user = NewUser(userId, r.DefaultLimit)
				r.Users[userId] = user
			}
			user.handleRejectedRequest(timestamp, *r)
		}
		return false
	}
}

func (r *RateLimiter) SetPenaltyRule(abuseThreshold int, cooldownDuration int64) {
	if abuseThreshold <= 0 || cooldownDuration <= 0 {
		return
	}
	r.AbuseThreshold = abuseThreshold
	r.CoolDownDuration = cooldownDuration
	r.PenaltyOn = true
}

func (r *RateLimiter) ConfigureTokenBucket(userId string, capacity int, refillRatePerSec int) {
	if capacity <= 0 || refillRatePerSec <= 0 {
		return
	}

	user, ok := r.Users[userId]
	if !ok {
		user = NewUser(userId, r.DefaultLimit)
		r.Users[userId] = user
	}

	user.TokenBucket.Enabled = true
	user.TokenBucket.Capacity = capacity
	user.TokenBucket.RefillRatePerSec = refillRatePerSec
	user.TokenBucket.LastRefillTimestamp = 0
	user.TokenBucket.Tokens = capacity
}

func (r *RateLimiter) AllowTokenBucketRequest(timestamp int64, userId string, tokensRequired int) bool {
	if tokensRequired <= 0 {
		return false
	}

	user, ok := r.Users[userId]
	if !ok {
		user = NewUser(userId, r.DefaultLimit)
		r.Users[userId] = user
	}

	if !user.TokenBucket.Enabled {
		return false
	}

	if r.PenaltyOn && user.isBanned(timestamp) {
		return false
	}

	tokens := int64(user.TokenBucket.Tokens)
	if user.LastRefillTimestamp != 0 {
		added := (timestamp - user.LastRefillTimestamp) * int64(user.TokenBucket.RefillRatePerSec) / 1000
		tokens = min(tokens, int64(user.Tokens)+added)
	}

	user.LastRefillTimestamp = timestamp

	if tokens >= int64(tokensRequired) {
		tokens -= int64(tokensRequired)
		user.Tokens = int(tokens)
		return true
	} else {
		if r.PenaltyOn {
			user.handleRejectedRequest(timestamp, *r)
		}
	}
	return false
}

func min(i, j int64) int64 {
	if i < j {
		return i
	}
	return j
}
