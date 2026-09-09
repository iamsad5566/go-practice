package main

type LRUCache struct {
	Source   map[string]*Node
	Capacity int
	Head     *Node
	Tail     *Node
}

type Node struct {
	Prev  *Node
	Next  *Node
	Key   string
	Value string
}

func NewNode(key, value string, prev, next *Node) *Node {
	return &Node{
		Prev:  prev,
		Next:  next,
		Key:   key,
		Value: value,
	}
}

func NewLRUCache(capacity int) *LRUCache {
	if capacity <= 0 {
		capacity = 1
	}

	head := NewNode("", "", nil, nil)
	tail := NewNode("", "", nil, nil)
	head.Next = tail
	tail.Prev = head
	return &LRUCache{
		Source:   make(map[string]*Node),
		Capacity: capacity,
		Head:     head,
		Tail:     tail,
	}
}

func (l *LRUCache) Get(key string) (string, bool) {
	node, ok := l.Source[key]
	// Guard Clauses
	if !ok {
		return "", false
	}

	l.moveOut(node)
	l.addToHead(node)
	return node.Value, true
}

func (l *LRUCache) Put(key string, value string) {
	node, ok := l.Source[key]
	if ok {
		node.Value = value
		l.moveOut(node)
		l.addToHead(node)
		return
	}

	node = NewNode(key, value, nil, nil)
	l.addToHead(node)
	l.Source[key] = node

	if len(l.Source) >= l.Capacity {

	}
}

func (l *LRUCache) moveOut(node *Node) {
	prev := node.Prev
	next := node.Next
	prev.Next = next
	next.Prev = prev
}

func (l *LRUCache) addToHead(node *Node) {
	sentinelH := l.Head
	prevH := sentinelH.Next

	node.Prev = sentinelH
	sentinelH.Next = node
	node.Next = prevH
}
