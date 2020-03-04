package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
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
	redisconfCompilers struct {
		redisHost    string
		redisPort    string
		redisDBCount int
	}
	conf struct {
		httpHost string
		httpPort string
	}
	comutex struct {
		mu        sync.Mutex
		compiling bool
	}
)

// Setting up flags
var (
	confdir            = flag.String("c", "conf.sample", "configuration directory")
	all                = flag.Bool("a", true, "run all parsers when set. Set by default")
	specific           = flag.String("o", "", "run only a specific parser [sshd]")
	debug              = flag.Bool("d", false, "debug info in logs")
	fromfile           = flag.String("f", "", "parse from file on disk")
	retry              = flag.Int("r", 1, "time in minute before retry on empty d4 queue")
	flush              = flag.Bool("F", false, "Flush HTML output, recompile all statistic from redis logs, then quits")
	redisD4            redis.Conn
	redisCompilers     *redis.Pool
	compilers          = [1]string{"sshd"}
	compilationTrigger = 20
	wg                 sync.WaitGroup
	compiling          comutex
	torun              = []logparser.Parser{}
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
		fmt.Printf("  Generate statistics about logs collected through d4 in HTML format.\n")
		fmt.Printf("  Logs should be groked and served as escaped JSON.\n")
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
	rp := redisconfCompilers{}
	flag.Parse()
	if flag.NFlag() == 0 || *confdir == "" {
		flag.Usage()
		os.Exit(1)
	} else {
		*confdir = strings.TrimSuffix(*confdir, "/")
		*confdir = strings.TrimSuffix(*confdir, "\\")
	}

	// Debug log
	if *debug {
		log.SetFlags(log.LstdFlags | log.Lshortfile)
	}

	// Dont't touch D4 server if Flushing
	if !*flush {
		// Parse Redis D4 Config
		tmp := config.ReadConfigFile(*confdir, "redis_d4")
		ss := strings.Split(string(tmp), "/")
		if len(ss) <= 1 {
			log.Fatal("Missing Database in Redis D4 config: should be host:port/database_name")
		}
		rd4.redisDB, _ = strconv.Atoi(ss[1])
		var ret bool
		ret, ss[0] = config.IsNet(ss[0])
		if ret {
			sss := strings.Split(string(ss[0]), ":")
			rd4.redisHost = sss[0]
			rd4.redisPort = sss[1]
		} else {
			log.Fatal("Redis config error.")
		}

		rd4.redisQueue = string(config.ReadConfigFile(*confdir, "redis_queue"))
		// Connect to D4 Redis
		// TODO use DialOptions to Dial with a timeout
		redisD4, err = redis.Dial("tcp", rd4.redisHost+":"+rd4.redisPort, redis.DialDatabase(rd4.redisDB))
		if err != nil {
			log.Fatal(err)
		}
		defer redisD4.Close()
	}

	// Parse Redis Compilers Config
	tmp := config.ReadConfigFile(*confdir, "redis_compilers")
	ss := strings.Split(string(tmp), "/")
	if len(ss) <= 1 {
		log.Fatal("Missing Database Count in Redis config: should be host:port/max number of DB")
	}
	rp.redisDBCount, _ = strconv.Atoi(ss[1])
	var ret bool
	ret, ss[0] = config.IsNet(ss[0])
	if ret {
		sss := strings.Split(string(ss[0]), ":")
		rp.redisHost = sss[0]
		rp.redisPort = sss[1]
	} else {
		log.Fatal("Redis config error.")
	}

	// Create a connection Pool
	redisCompilers = newPool(rp.redisHost+":"+rp.redisPort, rp.redisDBCount)

	// Line counter to trigger HTML compilation
	nblines := 0

	// Init parser depending on the parser flags:
	if *all {
		// Init all parsers
		for _, v := range compilers {
			switch v {
			case "sshd":
				sshdrcon1, err := redisCompilers.Dial()
				if err != nil {
					log.Fatal("Could not connect to Line one Redis")
				}
				sshdrcon2, err := redisCompilers.Dial()
				if err != nil {
					log.Fatal("Could not connect to Line two Redis")
				}
				sshd := logparser.SshdParser{}
				sshd.Set(&sshdrcon1, &sshdrcon2)
				torun = append(torun, &sshd)
			}
		}
	} else if *specific != "" {
		log.Println("TODO should run specific parser here")
	}

	// If we flush, we bypass the parsing loop
	if *flush {
		for _, v := range torun {
			err := v.Flush()
			if err != nil {
				log.Fatal(err)
			}
			compile()
		}
		// Parsing loop
	} else if *fromfile != "" {
		f, err = os.Open(*fromfile)
		if err != nil {
			log.Fatalf("Error opening seed file: %v", err)
		}
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			logline := scanner.Text()
			for _, v := range torun {
				err := v.Parse(logline)
				if err != nil {
					log.Fatal(err)
				}
			}
			nblines++
			if nblines > compilationTrigger {
				nblines = 0
				// Non-blocking
				if !compiling.compiling {
					go compile()
				}
			}
		}
	} else {
		// Pop D4 redis queue
		for {
			logline, err := redis.String(redisD4.Do("LPOP", "analyzer:3:"+rd4.redisQueue))
			if err == redis.ErrNil {
				// redis queue empty, let's sleep for a while
				time.Sleep(time.Duration(*retry) * time.Minute)
			} else if err != nil {
				log.Fatal(err)
				// let's parse
			} else {
				for _, v := range torun {
					err := v.Parse(logline)
					if err != nil {
						log.Println(err)
					}
				}
				nblines++
				if nblines > compilationTrigger {
					nblines = 0
					// Non-blocking
					if !compiling.compiling {
						go compile()
					}
				}
			}
		}
	}

	wg.Wait()
	log.Println("Exit")
}

func compile() {
	compiling.mu.Lock()
	compiling.compiling = true
	wg.Add(1)

	log.Println("Compiling")

	for _, v := range torun {
		err := v.Compile()
		if err != nil {
			log.Fatal(err)
		}
	}

	log.Println("Done")
	compiling.compiling = false
	compiling.mu.Unlock()
	wg.Done()
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
