package logcompiler

import (
	"io"
	"sync"

	"github.com/gomodule/redigo/redis"
)

type (
	// Compiler provides the interface for a Compiler
	// It should provide:
	//  Set to assign a redis connection to it
	//  Parse to parse a line of log
	//  Flush recomputes statisitcs and recompile output
	Compiler interface {
		Set(*sync.WaitGroup, *redis.Conn, *redis.Conn, io.Reader, int, *sync.WaitGroup)
		Pull() error
		Flush() error
		Compile() error
	}

	// CompilerStruct will implements Compiler, and should be embedded in
	// each type implementing compiler
	CompilerStruct struct {
		// Compiler redis Read
		r0 *redis.Conn
		// Compiler redis Write
		r1 *redis.Conn
		// Input Reader
		reader io.Reader
		// Number of line to process before triggering output
		compilationTrigger int
		// Current line processed
		nbLines int
		// Global WaitGroup to handle graceful exiting a compilation routines
		compilegr *sync.WaitGroup
		// Comutex embedding
		comutex
	}

	comutex struct {
		mu        sync.Mutex
		compiling bool
	}
)

// Set set the redis connections to this compiler
func (s *CompilerStruct) Set(wg *sync.WaitGroup, rconn0 *redis.Conn, rconn1 *redis.Conn, reader io.Reader, ct int, compilegr *sync.WaitGroup) {
	s.r0 = rconn0
	s.r1 = rconn1
	s.reader = reader
	s.compilationTrigger = ct
	s.compiling = false
	s.compilegr = compilegr
}
