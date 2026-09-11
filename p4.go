package main

import (
	"fmt"
	"sort"
	"sync/atomic"
)

type Side string
type Status string

const (
	BUY  Side = "BUY"
	SELL Side = "SELL"

	STATUS_OPEN             Status = "OPEN"
	STATUS_CANCELLED        Status = "CANCELLED"
	STATUS_FILLED           Status = "FILLED"
	STATUS_PARTIALLY_FILLED Status = "PARTIALLY_FILLED"
)

type Engine struct {
	Orders       map[string]*Order
	Trades       map[string]*Trade
	SerialNumber int64
}

type Order struct {
	ID        string
	Side      Side
	Price     int64
	Amount    int64
	Timestamp int64
	Status    Status
}

type Trade struct {
	TradeID     string
	BuyOrderID  string
	SellOrderID string
	Price       int64
	Amount      int64
	Timestamp   int64
}

func NewEngine() *Engine {
	return &Engine{
		Orders:       make(map[string]*Order),
		Trades:       make(map[string]*Trade),
		SerialNumber: 0,
	}
}

func NewOrder(orderId string, side Side, price, amount, timestamp int64) *Order {
	return &Order{
		ID:        orderId,
		Side:      side,
		Price:     price,
		Amount:    amount,
		Timestamp: timestamp,
		Status:    STATUS_OPEN,
	}
}

func NewTrade(tradeId string, buyOrderID, sellOrderID string, price, amount, timestamp int64) *Trade {
	return &Trade{
		TradeID:     tradeId,
		BuyOrderID:  buyOrderID,
		SellOrderID: sellOrderID,
		Price:       price,
		Amount:      amount,
		Timestamp:   timestamp,
	}
}

func min(i, j int64) int64 {
	if i < j {
		return i
	}
	return j
}

func (o *Order) setStatus(status Status) {
	o.Status = status
}

func (o *Order) copy() *Order {
	return &Order{
		ID:        o.ID,
		Side:      o.Side,
		Price:     o.Price,
		Amount:    o.Amount,
		Timestamp: o.Timestamp,
		Status:    o.Status,
	}
}

func (s Side) isValidSide() bool {
	if s != BUY && s != SELL {
		return false
	}
	return true
}

func (e *Engine) getBuyOrdesDESC() []*Order {
	orders := make([]*Order, 0)
	for _, v := range e.Orders {
		if v.Side == BUY && (v.Status == STATUS_OPEN || v.Status == STATUS_PARTIALLY_FILLED) {
			orders = append(orders, v)
		}
	}
	sort.Slice(orders, func(i, j int) bool {
		if orders[i].Price != orders[j].Price {
			return orders[i].Price > orders[j].Price
		}

		if orders[i].Timestamp != orders[j].Timestamp {
			return orders[i].Timestamp < orders[j].Timestamp
		}

		return orders[i].ID < orders[j].ID
	})

	return orders
}

func (e *Engine) getSellOrdersASC() []*Order {
	orders := make([]*Order, 0)
	for _, v := range e.Orders {
		if v.Side == SELL && (v.Status == STATUS_OPEN || v.Status == STATUS_PARTIALLY_FILLED) {
			orders = append(orders, v)
		}
	}
	sort.Slice(orders, func(i, j int) bool {
		if orders[i].Price != orders[j].Price {
			return orders[i].Price < orders[j].Price
		}

		if orders[i].Timestamp != orders[j].Timestamp {
			return orders[i].Timestamp < orders[j].Timestamp
		}

		return orders[i].ID < orders[j].ID
	})

	return orders
}

func (e *Engine) PlaceOrder(orderId string, side Side, price int64, amount int64, timestamp int64) bool {
	if orderId == "" || price <= 0 || amount <= 0 || !side.isValidSide() {
		return false
	}

	if _, ok := e.Orders[orderId]; ok {
		return false
	}

	order := NewOrder(orderId, side, price, amount, timestamp)

	// match
	if side == BUY {
		e.buyerMatch(order)
	} else {
		e.sellerMatch(order)
	}

	if order.Amount == amount {
		order.setStatus(STATUS_OPEN)
	}

	e.Orders[orderId] = order
	return true
}

func (e *Engine) CancelOrder(orderId string) bool {
	order, ok := e.Orders[orderId]
	if !ok || (order.Status != STATUS_OPEN && order.Status != STATUS_PARTIALLY_FILLED) {
		return false
	}

	order.setStatus(STATUS_CANCELLED)
	return true
}

func (e *Engine) GetOrder(orderId string) (*Order, bool) {
	order, ok := e.Orders[orderId]
	if !ok {
		return nil, false
	}
	return order.copy(), true
}

func (e *Engine) GetActiveOrdersCount(side Side) int {
	num := 0

	for _, v := range e.Orders {
		if v.Side == side && (v.Status == STATUS_OPEN || v.Status == STATUS_PARTIALLY_FILLED) {
			num++
		}
	}

	return num
}

func (e *Engine) buyerMatch(take *Order) {
	maker := e.getSellOrdersASC()
	for _, p := range maker {
		if take.Amount == 0 {
			take.setStatus(STATUS_FILLED)
			break
		}

		if take.Price >= p.Price {
			tradeID := fmt.Sprintf("T-%d", atomic.AddInt64(&e.SerialNumber, 1))
			trade := NewTrade(tradeID, take.ID, p.ID, p.Price, min(take.Amount, p.Amount), take.Timestamp)

			dealAmount := min(take.Amount, p.Amount)
			take.Amount -= dealAmount
			take.setStatus(STATUS_PARTIALLY_FILLED)

			p.Amount -= dealAmount
			if p.Amount == 0 {
				p.setStatus(STATUS_FILLED)
			} else {
				p.setStatus(STATUS_PARTIALLY_FILLED)
			}
			e.Trades[tradeID] = trade
		} else {
			e.Orders[take.ID] = take
			break
		}
	}
	if take.Amount == 0 {
		take.setStatus(STATUS_FILLED)
	}
}

func (e *Engine) sellerMatch(take *Order) {
	maker := e.getBuyOrdesDESC()
	for _, p := range maker {
		if take.Amount == 0 {
			take.setStatus(STATUS_FILLED)
			break
		}

		if take.Price <= p.Price {
			tradeID := fmt.Sprintf("T-%d", atomic.AddInt64(&e.SerialNumber, 1))
			trade := NewTrade(tradeID, p.ID, take.ID, p.Price, min(take.Amount, p.Amount), take.Timestamp)

			dealAmount := min(take.Amount, p.Amount)
			take.Amount -= dealAmount
			take.setStatus(STATUS_PARTIALLY_FILLED)

			p.Amount -= dealAmount
			if p.Amount == 0 {
				p.setStatus(STATUS_FILLED)
			} else {
				p.setStatus(STATUS_PARTIALLY_FILLED)
			}
			e.Trades[tradeID] = trade
		} else {
			e.Orders[take.ID] = take
			break
		}
	}
}

func (e *Engine) GetTrades() []Trade {
	res := make([]Trade, 0)
	trades := e.Trades
	if len(trades) == 0 {
		return res
	}

	for _, v := range trades {
		res = append(res, *v)
	}

	return res
}

func runLevel1Tests() {
	engine := NewEngine()

	// Test 1: 參數邊界檢驗
	assert(!engine.PlaceOrder("", BUY, 100, 10, 1000), "Test 1.1: Empty orderId")
	assert(!engine.PlaceOrder("o1", "INVALID", 100, 10, 1000), "Test 1.2: Invalid side")
	assert(!engine.PlaceOrder("o1", BUY, 0, 10, 1000), "Test 1.3: Zero price")
	assert(!engine.PlaceOrder("o1", BUY, 100, 0, 1000), "Test 1.4: Zero amount")
	assert(!engine.PlaceOrder("o1", BUY, -10, 10, 1000), "Test 1.5: Negative price")

	// Test 2: 正常下單與查詢
	assert(engine.PlaceOrder("o1", BUY, 100, 10, 1000), "Test 2.1: Place valid BUY order")
	assert(engine.PlaceOrder("o2", SELL, 105, 20, 1001), "Test 2.2: Place valid SELL order")
	assert(!engine.PlaceOrder("o1", BUY, 100, 5, 1002), "Test 2.3: Duplicate orderId fails")

	ord1, ok := engine.GetOrder("o1")
	assert(ok && ord1.Status == STATUS_OPEN && ord1.Price == 100 && ord1.Amount == 10, "Test 2.4: Get o1 correct")

	// Test 3: 活躍訂單計數
	assert(engine.GetActiveOrdersCount(BUY) == 1, "Test 3.1: 1 active BUY")
	assert(engine.GetActiveOrdersCount(SELL) == 1, "Test 3.2: 1 active SELL")

	// Test 4: 取消訂單
	assert(engine.CancelOrder("o1"), "Test 4.1: Cancel o1 success")
	assert(!engine.CancelOrder("o1"), "Test 4.2: Cancel already cancelled order fails")
	assert(!engine.CancelOrder("non_existent"), "Test 4.3: Cancel non-existent order fails")

	ord1Cancelled, ok := engine.GetOrder("o1")
	assert(ok && ord1Cancelled.Status == STATUS_CANCELLED, "Test 4.4: o1 status is CANCELLED")
	assert(engine.GetActiveOrdersCount(BUY) == 0, "Test 4.5: 0 active BUY after cancel")

	fmt.Println(">>> Level 1 Tests Passed! <<<")
}

func runLevel2Tests() {
	engine := NewEngine()

	// Maker 1 & 2 掛單: SELL 100 @ $10, SELL 50 @ $10 (同價，時間優先)
	// o_s1 在 t=1000 進來，o_s2 在 t=1001 進來
	assert(engine.PlaceOrder("o_s1", SELL, 10, 100, 1000), "Test 2.1: Place Maker SELL 1")
	assert(engine.PlaceOrder("o_s2", SELL, 10, 50, 1001), "Test 2.2: Place Maker SELL 2")
	// Maker 3: SELL 20 @ $12 (更高價)
	assert(engine.PlaceOrder("o_s3", SELL, 12, 20, 1002), "Test 2.3: Place Maker SELL 3")

	assert(engine.GetActiveOrdersCount(SELL) == 3, "Test 2.4: 3 active SELL orders")

	// Taker 1: BUY 120 @ $11 (t=2000)
	// 能匹配 $10，但不能匹配 $12
	// 依時間優先：先與 o_s1 吃滿 100 股，再與 o_s2 吃 20 股
	assert(engine.PlaceOrder("o_b1", BUY, 11, 120, 2000), "Test 2.5: Place Taker BUY")

	trades := engine.GetTrades()
	assert(len(trades) == 2, "Test 2.6: 2 trades executed")

	// Trade 1: o_b1 與 o_s1 成交 100 @ $10
	assert(trades[0].TradeID == "T-1" && trades[0].BuyOrderID == "o_b1" && trades[0].SellOrderID == "o_s1", "Test 2.7: Trade 1 parties")
	assert(trades[0].Price == 10 && trades[0].Amount == 100 && trades[0].Timestamp == 2000, "Test 2.8: Trade 1 terms")

	// Trade 2: o_b1 與 o_s2 成交 20 @ $10
	assert(trades[1].TradeID == "T-2" && trades[1].BuyOrderID == "o_b1" && trades[1].SellOrderID == "o_s2", "Test 2.9: Trade 2 parties")
	assert(trades[1].Price == 10 && trades[1].Amount == 20 && trades[1].Timestamp == 2000, "Test 2.10: Trade 2 terms")

	// 檢查狀態
	os1, _ := engine.GetOrder("o_s1")
	assert(os1.Status == STATUS_FILLED && os1.Amount == 0, "Test 2.11: o_s1 FILLED")

	os2, _ := engine.GetOrder("o_s2")
	assert(os2.Status == STATUS_PARTIALLY_FILLED && os2.Amount == 30, "Test 2.12: o_s2 PARTIALLY_FILLED with 30 left")

	ob1, _ := engine.GetOrder("o_b1")
	assert(ob1.Status == STATUS_FILLED && ob1.Amount == 0, "Test 2.13: o_b1 FILLED")

	// 活躍訂單計數：o_s2 (30) 與 o_s3 (20) 仍活躍，買單 0 張活躍
	assert(engine.GetActiveOrdersCount(SELL) == 2, "Test 2.14: 2 active SELL remaining")
	assert(engine.GetActiveOrdersCount(BUY) == 0, "Test 2.15: 0 active BUY remaining")

	// Taker 2: SELL 100 @ $9 (t=3000)
	// 當前買盤無任何活躍訂單，無法撮合，直接掛單
	assert(engine.PlaceOrder("o_s4", SELL, 9, 100, 3000), "Test 2.16: Place SELL with no match")
	os4, _ := engine.GetOrder("o_s4")
	assert(os4.Status == STATUS_OPEN && os4.Amount == 100, "Test 2.17: o_s4 remains OPEN")
	assert(len(engine.GetTrades()) == 2, "Test 2.18: No new trades")

	fmt.Println(">>> Level 2 Tests Passed! <<<")
}

func assert(cond bool, msg string) {
	if !cond {
		panic(msg)
	}
}

func main() {
	runLevel1Tests()
	runLevel2Tests()
}
