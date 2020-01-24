package logparser

import "github.com/gomodule/redigo/redis"

// Sshd is a struct that corresponds to a line
type Sshd struct {
	Date string
	Host string
	User string
	Src  string
}

// SshdParser Holds a struct that corresponds to a sshd log line
// and the redis connection
type SshdParser struct {
	logs Sshd
	r    *redis.Conn
}

// New Creates a new sshd parser
func New(rconn *redis.Conn) *SshdParser {
	return &SshdParser{
		logs: Sshd{},
		r:    rconn,
	}
}

// Parse parses a line of sshd log
func (s *SshdParser) Parse() error {
	//TODO
	return nil
}

// Push pushed the parsed line into redis
func (s *SshdParser) Push() error {
	//TODO
	return nil
}

// Pop returns the list of attributes
func (s *SshdParser) Pop() map[string]string {
	//TODO
	return nil
}
