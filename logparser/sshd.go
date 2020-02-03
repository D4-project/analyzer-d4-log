package logparser

import (
	"fmt"
	"html/template"
	"log"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gomodule/redigo/redis"
	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/plotutil"
	"gonum.org/v1/plot/vg"
)

// SshdParser Holds a struct that corresponds to a sshd log line
// and the redis connection
type SshdParser struct {
	r1 *redis.Conn
	r2 *redis.Conn
}

// Set set the redic connection to this parser
func (s *SshdParser) Set(rconn1 *redis.Conn, rconn2 *redis.Conn) {
	s.r1 = rconn1
	s.r2 = rconn2
}

// Parse parses a line of sshd log
func (s *SshdParser) Parse(logline string) error {
	r := *s.r1
	re := regexp.MustCompile(`^(?P<date>[[:alpha:]]{3} {1,2}\d{1,2}\s\d{2}:\d{2}:\d{2}) (?P<host>[^ ]+) sshd\[[[:alnum:]]+\]: Invalid user (?P<username>.*) from (?P<src>.*$)`)
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
	// TODO Make this automatic or a config parameter
	loc, _ := time.LoadLocation("Europe/Luxembourg")
	parsedTime, _ := time.ParseInLocation("Jan  2 15:04:05 2006", md["date"], loc)
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

	// Daily
	dstr := fmt.Sprintf("%v%v%v", parsedTime.Year(), fmt.Sprintf("%02d", int(parsedTime.Month())), fmt.Sprintf("%02d", int(parsedTime.Day())))

	// Check current entry date as oldest if older than the current
	var oldest string
	if oldest, err = redis.String(r.Do("GET", "oldest")); err == redis.ErrNil {
		r.Do("SET", "oldest", dstr)
	} else if err != nil {
		r.Close()
		return err
	} else {
		// Check if dates are the same
		if oldest != dstr {
			// Check who is the oldest
			parsedOldest, _ := time.Parse("20060102", oldest)
			if parsedTime.Before(parsedOldest) {
				r.Do("SET", "oldest", dstr)
			}
		}
	}

	// Check current entry date as oldest if older than the current
	var newest string
	if newest, err = redis.String(r.Do("GET", "newest")); err == redis.ErrNil {
		r.Do("SET", "newest", dstr)
	} else if err != nil {
		r.Close()
		return err
	} else {
		// Check if dates are the same
		if newest != dstr {
			// Check who is the newest
			parsedNewest, _ := time.Parse("20060102", newest)
			if parsedTime.After(parsedNewest) {
				r.Do("SET", "newest", dstr)
			}
		}
	}

	err = compileStats(s, dstr, "daily", md["src"], md["username"], md["host"])
	if err != nil {
		r.Close()
		return err
	}

	// Monthly
	mstr := fmt.Sprintf("%v%v", parsedTime.Year(), fmt.Sprintf("%02d", int(parsedTime.Month())))
	err = compileStats(s, mstr, "daily", md["src"], md["username"], md["host"])
	if err != nil {
		r.Close()
		return err
	}

	// Yearly
	ystr := fmt.Sprintf("%v", parsedTime.Year())
	err = compileStats(s, ystr, "daily", md["src"], md["username"], md["host"])
	if err != nil {
		r.Close()
		return err
	}

	return nil
}

func compileStats(s *SshdParser, datestr string, mode string, src string, username string, host string) error {
	r := *s.r1
	_, err := redis.String(r.Do("ZINCRBY", fmt.Sprintf("%v:%v", datestr, "statssrc"), 1, src))
	if err != nil {
		r.Close()
		return err
	}
	_, err = redis.String(r.Do("ZINCRBY", fmt.Sprintf("%v:%v", datestr, "statsusername"), 1, username))
	if err != nil {
		r.Close()
		return err
	}
	_, err = redis.String(r.Do("ZINCRBY", fmt.Sprintf("%v:%v", datestr, "statshost"), 1, host))
	if err != nil {
		r.Close()
		return err
	}

	_, err = redis.Int(r.Do("SADD", fmt.Sprintf("toupdate:%v", mode), fmt.Sprintf("%v:%v", datestr, "statssrc")))
	if err != nil {
		r.Close()
		return err
	}
	_, err = redis.Int(r.Do("SADD", fmt.Sprintf("toupdate:%v", mode), fmt.Sprintf("%v:%v", datestr, "statsusername")))
	if err != nil {
		r.Close()
		return err
	}
	_, err = redis.Int(r.Do("SADD", fmt.Sprintf("toupdate:%v", mode), fmt.Sprintf("%v:%v", datestr, "statshost")))
	if err != nil {
		r.Close()
		return err
	}

	return nil
}

// Compile create graphs of the results
func (s *SshdParser) Compile() error {
	r := *s.r2

	// Pulling statistics from database 1
	if _, err := r.Do("SELECT", 1); err != nil {
		r.Close()
		return err
	}

	// List days for which we need to update statistics
	toupdateD, err := redis.Strings(r.Do("SMEMBERS", "toupdate:daily"))
	if err != nil {
		r.Close()
		return err
	}

	// Plot statistics for each day to update
	for _, v := range toupdateD {
		err = plotStats(s, v)
		if err != nil {
			r.Close()
			return err
		}
	}

	// List months for which we need to update statistics
	toupdateM, err := redis.Strings(r.Do("SMEMBERS", "toupdate:monthly"))
	if err != nil {
		r.Close()
		return err
	}

	// Plot statistics for each month to update
	for _, v := range toupdateM {
		err = plotStats(s, v)
		if err != nil {
			r.Close()
			return err
		}
	}

	// List years for which we need to update statistics
	toupdateY, err := redis.Strings(r.Do("SMEMBERS", "toupdate:yearly"))
	if err != nil {
		r.Close()
		return err
	}

	// Plot statistics for each year to update
	for _, v := range toupdateY {
		err = plotStats(s, v)
		if err != nil {
			r.Close()
			return err
		}
	}

	// Write html file for navigating plots
	const tpl = `
	<!DOCTYPE html>
	<html>
		<head>
			<meta charset="UTF-8">
			<title>{{.Title}}</title>
			<script>
				var currentType = "statsusername";
				var currentDay = {{.MaxDate}};
			</script>
			<script src="load.js"></script>
			<style>
			body {
				background: white
			}
			#imageholder {
				background: black;
				color: white;
				padding: 1em;
				position: absolute;
				top: 50%;
				left: 50%;
				margin-right: -50%;
				transform: translate(-50%, -40%)
			}
			span {
				float: left;
				clear: left;
			}
		  	</style>
		</head>
		<body onload="loadImage({{.Current}}, currentType)">
			<span>
			<label for="statsday">Statistics for: </label>
			<input id="statsday" type="date" value="{{.MaxDate}}" min="{{.MinDate}}" max="{{.MaxDate}}" onchange="currentDay = this.value.replace(/-/g, ''); loadImage(currentDay, currentType)"/>
			</span>
			<span>
			<label for="statstype">Type: </label>
			 <select selected="statsusername" onchange="currentType = this.value; loadImage(currentDay.replace(/-/g, ''), currentType)">
				<option value="statsusername">Usernames</option>
				<option value="statssrc">Sources</option>
				<option value="statshost">Hosts</option>
		 	 </select> 
			</span>
			<span id="imageholder"></span>
		</body>
	</html>`

	// Get oldest / newest entries
	var newest string
	var oldest string
	if newest, err = redis.String(r.Do("GET", "newest")); err == redis.ErrNil {
		r.Close()
		return err
	}
	if oldest, err = redis.String(r.Do("GET", "oldest")); err == redis.ErrNil {
		r.Close()
		return err
	}
	parsedOldest, _ := time.Parse("20060102", oldest)
	parsedNewest, _ := time.Parse("20060102", newest)
	parsedOldestStr := parsedOldest.Format("2006-01-02")
	parsedNewestStr := parsedNewest.Format("2006-01-02")

	check := func(err error) {
		if err != nil {
			log.Fatal(err)
		}
	}
	t, err := template.New("webpage").Parse(tpl)
	check(err)

	data := struct {
		Title   string
		Current string
		MinDate string
		MaxDate string
	}{
		Title:   "sshd failed logins statistics",
		MinDate: parsedOldestStr,
		MaxDate: parsedNewestStr,
		Current: newest,
	}
	f, err := os.OpenFile("statistics.html", os.O_RDWR|os.O_CREATE, 0666)
	defer f.Close()
	err = t.Execute(f, data)
	check(err)

	return nil
}

func plotStats(s *SshdParser, v string) error {
	r := *s.r2
	zrank, err := redis.Strings(r.Do("ZRANGEBYSCORE", v, "-inf", "+inf", "WITHSCORES"))
	if err != nil {
		r.Close()
		return err
	}

	// Split keys and values - keep these ordered
	values := plotter.Values{}
	keys := make([]string, 0, len(zrank)/2)

	for k, v := range zrank {
		// keys
		if (k % 2) == 0 {
			keys = append(keys, zrank[k])
			// values
		} else {
			fv, _ := strconv.ParseFloat(v, 64)
			values = append(values, fv)
		}
	}

	p, err := plot.New()
	if err != nil {
		panic(err)
	}

	stype := strings.Split(v, ":")
	switch stype[1] {
	case "statsusername":
		p.Title.Text = "Usernames"
	case "statssrc":
		p.Title.Text = "Source IP"
	case "statshost":
		p.Title.Text = "Host"
	default:
		p.Title.Text = ""
		log.Println("We should not reach this point, open an issue.")
	}

	p.Y.Label.Text = "Count"
	w := 0.5 * vg.Centimeter
	bc, err := plotter.NewBarChart(values, w)
	bc.Horizontal = true
	if err != nil {
		return err
	}
	bc.LineStyle.Width = vg.Length(0)
	bc.Color = plotutil.Color(0)

	p.Add(bc)
	p.NominalY(keys...)

	// Create folder to store plots
	if _, err := os.Stat("data"); os.IsNotExist(err) {
		err := os.Mkdir("data", 0700)
		if err != nil {
			return err
		}
	}

	if _, err := os.Stat(filepath.Join("data", "sshd")); os.IsNotExist(err) {
		err := os.Mkdir(filepath.Join("data", "sshd"), 0700)
		if err != nil {
			return err
		}
	}

	if _, err := os.Stat(filepath.Join("data", "sshd", stype[0])); os.IsNotExist(err) {
		err := os.Mkdir(filepath.Join("data", "sshd", stype[0]), 0700)
		if err != nil {
			return err
		}
	}

	xsize := 3 + vg.Length(math.Round(float64(len(keys)/2)))
	if err := p.Save(15*vg.Centimeter, xsize*vg.Centimeter, filepath.Join("data", "sshd", stype[0], fmt.Sprintf("%v.svg", v))); err != nil {
		return err
	}

	return nil
}
