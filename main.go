package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"

	config "github.com/D4-project/d4-golang-utils/config"
	"github.com/gomodule/redigo/redis"
)

type (
	conf struct {
		redisHost  string
		redisPort  string
		redisDB    int
		redisQueue string
		httpHost   string
		httpPort   string
	}
)

// Setting up flags
var (
	confdir = flag.String("c", "conf.sample", "configuration directory")
	cr      redis.Conn
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
		fmt.Printf(" mandatory: redis - host:port/db\n")
		fmt.Printf(" mandatory: redis_queue - uuid\n")
		fmt.Printf(" optional: http_server - host:port\n\n")
		fmt.Printf("See conf.sample for an example.\n")
	}

	// Config
	c := conf{}
	flag.Parse()
	if flag.NFlag() == 0 || *confdir == "" {
		flag.Usage()
		os.Exit(1)
	} else {
		*confdir = strings.TrimSuffix(*confdir, "/")
		*confdir = strings.TrimSuffix(*confdir, "\\")
	}

	// Parse Redis Config
	tmp := config.ReadConfigFile(*confdir, "redis")
	ss := strings.Split(string(tmp), "/")
	if len(ss) <= 1 {
		log.Fatal("Missing Database in Redis config: should be host:port/database_name")
	}
	c.redisDB, _ = strconv.Atoi(ss[1])
	var ret bool
	ret, ss[0] = config.IsNet(ss[0])
	if !ret {
		sss := strings.Split(string(ss[0]), ":")
		c.redisHost = sss[0]
		c.redisPort = sss[1]
	}
	c.redisQueue = string(config.ReadConfigFile(*confdir, "redis_queue"))
	initRedis(c.redisHost, c.redisPort, c.redisDB)
	defer cr.Close()

	log.Println("Exit")
}

func initRedis(host string, port string, d int) {
	err := errors.New("")
	cr, err = redis.Dial("tcp", host+":"+port, redis.DialDatabase(d))
	if err != nil {
		log.Fatal(err)
	}
}
