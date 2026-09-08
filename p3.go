package main

type RateLimiter struct {
	TimeWindow   int64 //ms
	DefaultLimit int
	Users        map[string]*User
	EndPoints    map[string]*EndPoint
}

type User struct {
	ID                 string
	Limit              int
	LastSuccessfulReqs []int64
}
type EndPoint struct {
	Route              string
	Limit              int
	LastSuccessfulReqs []int64
}

func NewUser(id string, limit int) *User {
	return &User{
		ID:                 id,
		Limit:              limit,
		LastSuccessfulReqs: make([]int64, 0, limit),
	}
}

func NewEndPoint(route string, limit int) *EndPoint {
	return &EndPoint{
		Route:              route,
		Limit:              limit,
		LastSuccessfulReqs: make([]int64, 0, limit),
	}
}

func (u *User) ResetRateLimit() {
	u.LastSuccessfulReqs = make([]int64, 0, u.Limit)
}

func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		TimeWindow:   1000,
		DefaultLimit: 5,
		Users:        make(map[string]*User),
		EndPoints:    make(map[string]*EndPoint),
	}
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
		r.Users[userId] = NewUser(userId, r.DefaultLimit)
		return true
	}

	validIdx := r.getValidIdx(timestamp, user.LastSuccessfulReqs)
	user.LastSuccessfulReqs = user.LastSuccessfulReqs[validIdx:]

	if len(user.LastSuccessfulReqs) < user.Limit {
		return true
	}

	return false
}

func (r *RateLimiter) AllowRequest(timestamp int64, userId string) bool {
	passed := r.QualifiedReq(timestamp, userId)
	if passed {
		r.updateLatestRequest(timestamp, &r.Users[userId].LastSuccessfulReqs)
		return true
	}

	return false
}

func (r *RateLimiter) updateLatestRequest(timestamp int64, reqs *[]int64) {
	*reqs = append(*reqs, timestamp)
}

func (r *RateLimiter) getValidIdx(timestamp int64, reqs []int64) int {
	cutOff := timestamp - r.TimeWindow
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
		r.updateLatestRequest(timestamp, &r.Users[userId].LastSuccessfulReqs)
		return true
	} else {
		validIdx := r.getValidIdx(timestamp, endPoint.LastSuccessfulReqs)
		endPoint.LastSuccessfulReqs = endPoint.LastSuccessfulReqs[validIdx:]
		if len(endPoint.LastSuccessfulReqs) < endPoint.Limit {
			r.updateLatestRequest(timestamp, &endPoint.LastSuccessfulReqs)
			r.updateLatestRequest(timestamp, &r.Users[userId].LastSuccessfulReqs)
			return true
		}
		return false
	}
}
