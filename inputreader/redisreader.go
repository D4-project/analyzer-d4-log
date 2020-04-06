package inputreader

import (
	"bytes"
	"io"
	"log"
	"time"

	"github.com/gomodule/redigo/redis"
)

// RedisLPOPReader is a abstraction of LPOP list
// and behaves likes a scanner
type RedisLPOPReader struct {
	// D4 redis connection
	r *redis.Conn
	// D4 redis database
	d int
	// D4 Queue storing
	q string
	// Time in minute before retrying
	retryPeriod time.Duration
	// Current buffer
	buf []byte
}

// NewLPOPReader creates a new RedisLPOPScanner
func NewLPOPReader(rc *redis.Conn, db int, queue string, rt int) *RedisLPOPReader {
	rr := *rc

	if _, err := rr.Do("SELECT", db); err != nil {
		rr.Close()
		log.Fatal(err)
	}

	return &RedisLPOPReader{
		r:           rc,
		d:           db,
		q:           queue,
		retryPeriod: time.Duration(rt) * time.Minute,
	}
}

// Read LPOP the redis queue and use a bytes reader to copy
// the resulting data in p
func (rl *RedisLPOPReader) Read(p []byte) (n int, err error) {
	rr := *rl.r

	buf, err := redis.Bytes(rr.Do("LPOP", rl.q))
	// If redis return empty: EOF (user should not stop)
	if err == redis.ErrNil {
		return 0, io.EOF
	} else if err != nil {
		log.Println(err)
		return 0, err
	}
	rreader := bytes.NewReader(buf)
	n, err = rreader.Read(p)
	return n, err
}

// Teardown is called on error to close the redis connection
func (rl *RedisLPOPReader) Teardown() {
	(*rl.r).Close()
}
