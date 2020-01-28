package logparser

import (
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/gomodule/redigo/redis"
)

// SshdParser Holds a struct that corresponds to a sshd log line
// and the redis connection
type SshdParser struct {
	r *redis.Conn
}

// Set set the redic connection to this parser
func (s *SshdParser) Set(rconn *redis.Conn) {
	s.r = rconn
}

// Parse parses a line of sshd log
func (s *SshdParser) Parse(logline string) error {
	r := *s.r
	re := regexp.MustCompile(`^(?P<date>[[:alpha:]]{3}\s\d{2}\s\d{2}:\d{2}:\d{2}) (?P<host>[^ ]+) sshd\[[[:alnum:]]+\]: Invalid user (?P<username>[^ ]+) from (?P<src>.*$)`)
	n1 := re.SubexpNames()
	r2 := re.FindAllStringSubmatch(logline, -1)[0]

	// Build the group map for the line
	md := map[string]string{}
	for i, n := range r2 {
		// fmt.Printf("%d. match='%s'\tname='%s'\n", i, n, n1[i])
		md[n1[i]] = n
	}

	// Assumes the system parses logs recorded during the current year
	md["date"] = fmt.Sprintf("%v %v", md["date"], time.Now().Year())
	// Make this automatic or a config parameter
	loc, _ := time.LoadLocation("Europe/Luxembourg")
	parsedTime, _ := time.ParseInLocation("Jan 02 15:04:05 2006", md["date"], loc)
	md["date"] = string(strconv.FormatInt(parsedTime.Unix(), 10))

	// Pushing loglines in database 0
	if _, err := r.Do("SELECT", 0); err != nil {
		r.Close()
		return err
	}
	_, err := redis.Bool(r.Do("HSET", fmt.Sprintf("%v:%v", md["date"], md["host"]), "username", md["username"], "src", md["src"]))
	if err != nil {
		r.Close()
		return err
	}

	// Pushing statistics in database 1
	if _, err := r.Do("SELECT", 1); err != nil {
		r.Close()
		return err
	}
	_, err = redis.String(r.Do("ZINCRBY", fmt.Sprintf("%v%v%v:statssrc", parsedTime.Year(), int(parsedTime.Month()), parsedTime.Day()), 1, md["src"]))
	if err != nil {
		r.Close()
		return err
	}
	_, err = redis.String(r.Do("ZINCRBY", fmt.Sprintf("%v%v%v:statsusername", parsedTime.Year(), int(parsedTime.Month()), parsedTime.Day()), 1, md["username"]))
	if err != nil {
		r.Close()
		return err
	}
	_, err = redis.String(r.Do("ZINCRBY", fmt.Sprintf("%v%v%v:statshost", parsedTime.Year(), int(parsedTime.Month()), parsedTime.Day()), 1, md["host"]))
	if err != nil {
		r.Close()
		return err
	}

	return nil
}
