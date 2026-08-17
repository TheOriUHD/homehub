package minecraft

import "sync"

// SeqLine is a console line with its position in the instance's stream.
// Sequence numbers are monotonic per session and never reused, so a client
// polling "everything after seq N" gets exactly the missed lines.
type SeqLine struct {
	Seq   int64  `json:"seq"`
	TS    int64  `json:"ts"`
	Level string `json:"level,omitempty"`
	Text  string `json:"text"`
}

// ConsoleBuffer is a fixed-capacity ring of console lines. The companion
// backfills logs/latest.log on connect, so the buffer holds the full
// scrollback from server start (up to capacity) plus everything live.
type ConsoleBuffer struct {
	mu    sync.RWMutex
	buf   []SeqLine
	start int   // index of the oldest line
	count int   // lines currently held
	next  int64 // next sequence number to assign
}

// consoleCapacity bounds memory per server: 100k lines ≈ 10–20 MB worst
// case, far beyond what any console UI needs to scroll.
const consoleCapacity = 100_000

func NewConsoleBuffer() *ConsoleBuffer {
	return &ConsoleBuffer{buf: make([]SeqLine, 0, 4096)}
}

// Append assigns sequence numbers and stores the lines, evicting the oldest
// beyond capacity. Returns the stored lines with sequences filled in.
func (c *ConsoleBuffer) Append(lines []ConsoleLine) []SeqLine {
	if len(lines) == 0 {
		return nil
	}
	out := make([]SeqLine, len(lines))
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, ln := range lines {
		sl := SeqLine{Seq: c.next, TS: ln.TS, Level: ln.Level, Text: ln.Text}
		c.next++
		if len(c.buf) < consoleCapacity {
			if c.count == len(c.buf) {
				c.buf = append(c.buf, sl)
			} else {
				c.buf[(c.start+c.count)%len(c.buf)] = sl
			}
			c.count++
		} else {
			c.buf[c.start] = sl
			c.start = (c.start + 1) % len(c.buf)
		}
		out[i] = sl
	}
	return out
}

// After returns up to limit lines with Seq > afterSeq, oldest first, plus
// the sequence of the oldest line still held (the "history begins here"
// marker for the UI) and the next sequence that will be assigned.
func (c *ConsoleBuffer) After(afterSeq int64, limit int) (lines []SeqLine, oldest, next int64) {
	if limit <= 0 || limit > 5000 {
		limit = 5000
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	next = c.next
	if c.count == 0 {
		return nil, 0, next
	}
	oldest = c.buf[c.start].Seq
	// First held sequence > afterSeq; sequences are dense, so index math.
	from := afterSeq + 1
	if from < oldest {
		from = oldest
	}
	if from >= c.next {
		return nil, oldest, next
	}
	idx := int(from - oldest)
	n := c.count - idx
	if n > limit {
		n = limit
	}
	lines = make([]SeqLine, n)
	for i := 0; i < n; i++ {
		lines[i] = c.buf[(c.start+idx+i)%len(c.buf)]
	}
	return lines, oldest, next
}

// Tail returns the newest limit lines, oldest first.
func (c *ConsoleBuffer) Tail(limit int) []SeqLine {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.count == 0 {
		return nil
	}
	if limit <= 0 || limit > c.count {
		limit = c.count
	}
	lines := make([]SeqLine, limit)
	first := c.count - limit
	for i := 0; i < limit; i++ {
		lines[i] = c.buf[(c.start+first+i)%len(c.buf)]
	}
	return lines
}
