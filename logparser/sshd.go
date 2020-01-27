package logparser

import (
	"fmt"
	"log"
	"regexp"
	"strconv"
	"time"

	"github.com/gomodule/redigo/redis"
)

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

	// Pushing logline in redis
	redislog := fmt.Sprintf("HMSET %v:%v username \"%v\" src \"%v\"", md["date"], md["host"], md["username"], md["src"])
	a, err := r.Do(redislog)
	fmt.Println(a)
	if err != nil {
		log.Fatal("Could connect to the Redis database")
	}
	today := time.Now()
	// Statistics
	dailysrc := fmt.Sprintf("ZINCBY %v%v%v:statssrc 1 %v", today.Year(), int(today.Month()), today.Day(), md["src"])
	_, err = r.Do(dailysrc)
	if err != nil {
		log.Fatal("Could connect to the Redis database")
	}
	dailyusername := fmt.Sprintf("ZINCBY %v%v%v:statsusername 1 %v", today.Year(), int(today.Month()), today.Day(), md["username"])
	fmt.Println(dailyusername)
	_, err = r.Do(dailyusername)
	if err != nil {
		log.Fatal("Could connect to the Redis database")
	}
	dailyhost := fmt.Sprintf("ZINCBY %v%v%v:statshost 1 %v", today.Year(), int(today.Month()), today.Day(), md["host"])
	_, err = r.Do(dailyhost)
	if err != nil {
		log.Fatal("Could connect to the Redis database")
	}

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
