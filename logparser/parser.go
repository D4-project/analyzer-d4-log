package logparser

import "github.com/gomodule/redigo/redis"

type (
	// Parser provides the interface for a Parser
	// It should provide:
	//  Set to assign a redis connection to it
	//  Parse to parse a line of log
	Parser interface {
		Set(*redis.Conn)
		Parse(string) error
	}
)
