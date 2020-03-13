package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/D4-project/analyzer-d4-log/inputreader"
	"github.com/D4-project/analyzer-d4-log/logcompiler"
	config "github.com/D4-project/d4-golang-utils/config"
	"github.com/gomodule/redigo/redis"
)

type (
	// Input is a grok - NIFI or Logstash
	redisconfInput struct {
		redisHost string
		redisPort string
		redisDB   int
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
)

// Setting up flags
var (
	// Flags
	confdir  = flag.String("c", "conf.sample", "configuration directory")
	all      = flag.Bool("a", true, "run all compilers when set. Set by default")
	specific = flag.String("o", "", "run only a specific parser [sshd]")
	debug    = flag.Bool("d", false, "debug info in logs")
	fromfile = flag.String("f", "", "parse from file on disk")
	retry    = flag.Int("r", 1, "time in minute before retry on empty d4 queue")
	flush    = flag.Bool("F", false, "Flush HTML output, recompile all statistic from redis logs, then quits")
	// Pools of redis connections
	redisCompilers *redis.Pool
	redisInput     *redis.Pool
	// Compilers
	compilers          = [1]string{"sshd"}
	compilationTrigger = 20
	torun              = []logcompiler.Compiler{}
	// Routine handling
	pullgr    sync.WaitGroup
	compilegr sync.WaitGroup
)

func main() {
	sortie := make(chan os.Signal, 1)
	signal.Notify(sortie, os.Interrupt, os.Kill)
	// Signal goroutine
	go func() {
		<-sortie
		fmt.Println("Exiting.")
		compilegr.Wait()
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
		fmt.Printf(" mandatory: redis_compilers - host:port/maxdb\n")
		fmt.Printf(" optional: http_server - host:port\n\n")
		fmt.Printf("See conf.sample for an example.\n")
	}

	// Config
	// c := conf{}
	ri := redisconfInput{}
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

	// Dont't touch input server if Flushing
	if !*flush {
		// Parse Input Redis Config
		tmp := config.ReadConfigFile(*confdir, "redis_input")
		ss := strings.Split(string(tmp), "/")
		if len(ss) <= 1 {
			log.Fatal("Missing Database in Redis input config: should be host:port/database_name")
		}
		ri.redisDB, _ = strconv.Atoi(ss[1])
		var ret bool
		ret, ss[0] = config.IsNet(ss[0])
		if ret {
			sss := strings.Split(string(ss[0]), ":")
			ri.redisHost = sss[0]
			ri.redisPort = sss[1]
		} else {
			log.Fatal("Redis config error.")
		}
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

	// Create a connection Pool for output Redis
	redisCompilers = newPool(rp.redisHost+":"+rp.redisPort, rp.redisDBCount)
	redisInput = newPool(ri.redisHost+":"+ri.redisPort, 16)

	// Create a chan to get the goroutines errors messages
	pullreturn := make(chan error, 1)
	// Launching Pull routines monitoring
	go func() {
		select {
		case err := <-pullreturn:
			log.Println(err)
			os.Exit(1)
			log.Println("Exit.")
		}
	}()

	// Init compiler depending on the compiler flags:
	if *all {
		// Init all compilers
		for _, v := range compilers {
			switch v {
			case "sshd":
				sshdrcon0, err := redisCompilers.Dial()
				if err != nil {
					log.Fatal("Could not connect to input line on Compiler Redis")
				}
				defer sshdrcon0.Close()
				sshdrcon1, err := redisCompilers.Dial()
				if err != nil {
					log.Fatal("Could not connect to output line on Compiler Redis")
				}
				defer sshdrcon1.Close()
				sshdrcon2, err := redisInput.Dial()
				if err != nil {
					log.Fatal("Could not connect to output line on Input Redis")
				}
				defer sshdrcon2.Close()
				redisReader := inputreader.NewLPOPReader(&sshdrcon2, ri.redisDB, "sshd", *retry)
				sshd := logcompiler.SSHDCompiler{}
				sshd.Set(&pullgr, &sshdrcon0, &sshdrcon1, redisReader, compilationTrigger, &compilegr, &pullreturn)
				torun = append(torun, &sshd)
			}
		}
	} else if *specific != "" {
		log.Println("TODO should run specific compiler here")
	}

	// If we flush, we bypass the compiling loop
	if *flush {
		for _, v := range torun {
			err := v.Flush()
			if err != nil {
				log.Fatal(err)
			}
			log.Println("Exit")
			os.Exit(0)
		}
	}

	// Launching Pull routines
	for _, v := range torun {

		// If we read from a file, we set the reader to os.open
		if *fromfile != "" {
			f, err = os.Open(*fromfile)
			if err != nil {
				log.Fatalf("Error opening seed file: %v", err)
			}
			defer f.Close()
			v.SetReader(f)
		}

		// we add pulling routines to a waitgroup,
		// they can immediately die when exiting.
		pullgr.Add(1)
		go v.Pull(pullreturn)
	}

	pullgr.Wait()
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
