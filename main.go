package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/D4-project/analyzer-d4-log/logparser"
	config "github.com/D4-project/d4-golang-utils/config"
	"github.com/gomodule/redigo/redis"
)

type (
	redisconfD4 struct {
		redisHost  string
		redisPort  string
		redisDB    int
		redisQueue string
	}
	redisconfParsers struct {
		redisHost    string
		redisPort    string
		redisDBCount int
	}
	conf struct {
		httpHost string
		httpPort string
	}
)

// Setting up flags
var (
	confdir      = flag.String("c", "conf.sample", "configuration directory")
	all          = flag.Bool("a", true, "run all parsers when set. Set by default")
	specific     = flag.String("o", "", "run only a specific parser [sshd]")
	redisD4      redis.Conn
	redisParsers *redis.Pool
	parsers      = [1]string{"sshd"}
)

func main() {
	sortie := make(chan os.Signal, 1)
	signal.Notify(sortie, os.Interrupt, os.Kill)
	// Signal goroutine
	go func() {
		<-sortie
		fmt.Println("Exiting.")
		log.Println("Exit")
		os.Exit(0)
	}()

	// Setting up log file
	f, err := os.OpenFile("analyzer-d4-log.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("error opening file: %v", err)
	}
	defer f.Close()
	log.SetOutput(f)
	log.Println("Init")

	// Usage and flags
	flag.Usage = func() {
		fmt.Printf("analyzer-d4-log:\n\n")
		fmt.Printf("  Generate statistics about logs collected through d4 in\n")
		fmt.Printf("  HTML format. Optionally serves the results over HTTP.\n")
		fmt.Printf("\n")
		flag.PrintDefaults()
		fmt.Printf("\n")
		fmt.Printf("The configuration directory should hold the following files\n")
		fmt.Printf("to specify the settings to use:\n\n")
		fmt.Printf(" mandatory: redis_d4 - host:port/db\n")
		fmt.Printf(" mandatory: redis_queue - uuid\n")
		fmt.Printf(" mandatory: redis_parsers - host:port/maxdb\n")
		fmt.Printf(" optional: http_server - host:port\n\n")
		fmt.Printf("See conf.sample for an example.\n")
	}

	// Config
	// c := conf{}
	rd4 := redisconfD4{}
	rp := redisconfParsers{}
	flag.Parse()
	if flag.NFlag() == 0 || *confdir == "" {
		flag.Usage()
		os.Exit(1)
	} else {
		*confdir = strings.TrimSuffix(*confdir, "/")
		*confdir = strings.TrimSuffix(*confdir, "\\")
	}

	// Parse Redis D4 Config
	tmp := config.ReadConfigFile(*confdir, "redis_d4")
	ss := strings.Split(string(tmp), "/")
	if len(ss) <= 1 {
		log.Fatal("Missing Database in Redis D4 config: should be host:port/database_name")
	}
	rd4.redisDB, _ = strconv.Atoi(ss[1])
	var ret bool
	ret, ss[0] = config.IsNet(ss[0])
	if !ret {
		sss := strings.Split(string(ss[0]), ":")
		rd4.redisHost = sss[0]
		rd4.redisPort = sss[1]
	}
	rd4.redisQueue = string(config.ReadConfigFile(*confdir, "redis_queue"))
	// Connect to D4 Redis
	redisD4, err = redis.Dial("tcp", rd4.redisHost+":"+rd4.redisPort, redis.DialDatabase(rd4.redisDB))
	if err != nil {
		log.Fatal(err)
	}
	defer redisD4.Close()

	// Parse Redis Parsers Config
	tmp = config.ReadConfigFile(*confdir, "redis_parsers")
	ss = strings.Split(string(tmp), "/")
	if len(ss) <= 1 {
		log.Fatal("Missing Database Count in Redis config: should be host:port/max number of DB")
	}
	rp.redisDBCount, _ = strconv.Atoi(ss[1])
	ret, ss[0] = config.IsNet(string(tmp))
	if !ret {
		sss := strings.Split(string(ss[0]), ":")
		rp.redisHost = sss[0]
		rp.redisPort = sss[1]
	}

	// Create a connection Pool
	redisParsers = newPool(rp.redisHost+":"+rp.redisPort, rp.redisDBCount)

	// Init parser depending on the parser flags:
	if *all {
		// Init all parsers
		var torun = []logparser.Parser{}
		for _, v := range parsers {
			switch v {
			case "sshd":
				var sshdrcon, err = redisParsers.Dial()
				if err != nil {
					log.Fatal("Could not connect to Parser Redis")
				}
				sshd := logparser.New(&sshdrcon)
				torun = append(torun, sshd)
			}
		}
	} else if *specific != "" {
		log.Println("TODO should run specific parser here")
	}

	// Run the parsers
	log.Println("TODO should run the parsers here")

	log.Println("Exit")
}

func newPool(addr string, maxconn int) *redis.Pool {
	return &redis.Pool{
		MaxActive:   maxconn,
		MaxIdle:     3,
		IdleTimeout: 240 * time.Second,
		// Dial or DialContext must be set. When both are set, DialContext takes precedence over Dial.
		Dial: func() (redis.Conn, error) { return redis.Dial("tcp", addr) },
	}
}
