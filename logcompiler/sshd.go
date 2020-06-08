package logcompiler

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gomodule/redigo/redis"
	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/plotutil"
	"gonum.org/v1/plot/vg"
)

// SSHDCompiler Holds a struct that corresponds to a sshd groked line
// and the redis connections
type SSHDCompiler struct {
	CompilerStruct
}

// GrokedSSHD map JSON fields to Go struct
type GrokedSSHD struct {
	SSHMessage      string `json:"ssh_message"`
	SyslogPid       string `json:"syslog_pid"`
	SyslogHostname  string `json:"syslog_hostname"`
	SyslogTimestamp string `json:"syslog_timestamp"`
	SshdClientIP    string `json:"sshd_client_ip"`
	SyslogProgram   string `json:"syslog_program"`
	SshdInvalidUser string `json:"sshd_invalid_user"`
}

type MISP_auth_failure_sshd_username struct {
	mtype    string `json:"type"`
	username string `json:"username"`
	total    string `json:"total"`
}

// Flush recomputes statistics and recompile HTML output
// TODO : review after refacto
func (s *SSHDCompiler) Flush() error {
	log.Println("Flushing")
	r1 := *s.r1
	r0 := *s.r0
	// writing in database 1
	if _, err := r1.Do("SELECT", 1); err != nil {
		s.teardown(err)
	}
	// flush stats DB
	if _, err := r1.Do("FLUSHDB"); err != nil {
		s.teardown(err)
	}
	log.Println("Statistics Database Flushed")

	// reading from database 0
	if _, err := r0.Do("SELECT", 0); err != nil {
		s.teardown(err)
	}

	// Compile statistics / html output for each line
	keys, err := redis.Strings(r0.Do("KEYS", "*"))
	if err != nil {
		s.teardown(err)
	}
	for _, v := range keys {
		dateHost := strings.Split(v, ":")
		kkeys, err := redis.StringMap(r0.Do("HGETALL", v))
		if err != nil {
			s.teardown(err)
		}

		dateInt, err := strconv.ParseInt(dateHost[0], 10, 64)
		if err != nil {
			s.teardown(err)
		}
		parsedTime := time.Unix(dateInt, 0)
		err = compileStats(s, parsedTime, kkeys["src"], kkeys["username"], dateHost[1])
		if err != nil {
			s.teardown(err)
		}
	}

	return nil
}

// Pull pulls a line of groked sshd logline from redis
func (s *SSHDCompiler) Pull(c chan error) {
	r1 := *s.r1

	for {
		jsoner := json.NewDecoder(s.reader)
	DecodeLoop:
		for jsoner.More() {
			var m GrokedSSHD
			err := jsoner.Decode(&m)
			if err := jsoner.Decode(&m); err == io.EOF {
				// On EOF we break this loop to go to a sleep
				break DecodeLoop
			} else if err != nil {
				s.teardown(err)
			}

			fmt.Printf("time: %s, hostname: %s, client_ip: %s, user: %s\n", m.SyslogTimestamp, m.SyslogHostname, m.SshdClientIP, m.SshdInvalidUser)

			// Assumes the system parses logs recorded during the current year
			m.SyslogTimestamp = fmt.Sprintf("%v %v", m.SyslogTimestamp, time.Now().Year())
			// TODO Make this automatic or a config parameter
			loc, _ := time.LoadLocation("Europe/Luxembourg")
			parsedTime, _ := time.ParseInLocation("Jan 2 15:04:05 2006", m.SyslogTimestamp, loc)
			m.SyslogTimestamp = string(strconv.FormatInt(parsedTime.Unix(), 10))

			// Pushing loglines in database 0
			if _, err := r1.Do("SELECT", 0); err != nil {
				s.teardown(err)
			}

			// Writing logs
			_, err = redis.Bool(r1.Do("HSET", fmt.Sprintf("%v:%v", m.SyslogTimestamp, m.SyslogHostname), "username", m.SshdInvalidUser, "src", m.SshdClientIP))
			if err != nil {
				s.teardown(err)
			}

			err = compileStats(s, parsedTime, m.SshdClientIP, m.SshdInvalidUser, m.SyslogHostname)
			if err != nil {
				s.teardown(err)
			}

			// Compiler html / jsons
			s.nbLines++
			if s.nbLines > s.compilationTrigger {
				s.nbLines = 0
				//Non-blocking
				if !s.compiling {
					go s.compile()
				}
			}
		}

		// EOF, we wait for the reader to have
		// new data available
		time.Sleep(s.retryPeriod)
	}
}

func compileStats(s *SSHDCompiler, parsedTime time.Time, src string, username string, host string) error {
	r := *s.r1

	// Pushing statistics in database 1
	if _, err := r.Do("SELECT", 1); err != nil {
		s.teardown(err)
	}

	// Daily
	dstr := fmt.Sprintf("%v%v%v", parsedTime.Year(), fmt.Sprintf("%02d", int(parsedTime.Month())), fmt.Sprintf("%02d", int(parsedTime.Day())))

	// Check current entry date as oldest if older than the current
	if oldest, err := redis.String(r.Do("GET", "oldest")); err == redis.ErrNil {
		r.Do("SET", "oldest", dstr)
	} else if err != nil {
		s.teardown(err)
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
	if newest, err := redis.String(r.Do("GET", "newest")); err == redis.ErrNil {
		r.Do("SET", "newest", dstr)
	} else if err != nil {
		s.teardown(err)
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

	err := compileStat(s, dstr, "daily", src, username, host)
	if err != nil {
		s.teardown(err)
	}

	// Monthly
	mstr := fmt.Sprintf("%v%v", parsedTime.Year(), fmt.Sprintf("%02d", int(parsedTime.Month())))
	err = compileStat(s, mstr, "daily", src, username, host)
	if err != nil {
		s.teardown(err)
	}

	// Yearly
	ystr := fmt.Sprintf("%v", parsedTime.Year())
	err = compileStat(s, ystr, "daily", src, username, host)
	if err != nil {
		s.teardown(err)
	}

	return nil
}

func compileStat(s *SSHDCompiler, datestr string, mode string, src string, username string, host string) error {
	r := *s.r1
	_, err := redis.String(r.Do("ZINCRBY", fmt.Sprintf("%v:%v", datestr, "statssrc"), 1, src))
	if err != nil {
		return err
	}
	_, err = redis.String(r.Do("ZINCRBY", fmt.Sprintf("%v:%v", datestr, "statsusername"), 1, username))
	if err != nil {
		return err
	}
	_, err = redis.String(r.Do("ZINCRBY", fmt.Sprintf("%v:%v", datestr, "statshost"), 1, host))
	if err != nil {
		return err
	}

	_, err = redis.Int(r.Do("SADD", fmt.Sprintf("toupdate:%v", mode), fmt.Sprintf("%v:%v", datestr, "statssrc")))
	if err != nil {
		return err
	}
	_, err = redis.Int(r.Do("SADD", fmt.Sprintf("toupdate:%v", mode), fmt.Sprintf("%v:%v", datestr, "statsusername")))
	if err != nil {
		return err
	}
	_, err = redis.Int(r.Do("SADD", fmt.Sprintf("toupdate:%v", mode), fmt.Sprintf("%v:%v", datestr, "statshost")))
	if err != nil {
		return err
	}

	return nil
}

// compile create json and graphical representation of the results
func (s *SSHDCompiler) compile() error {
	s.mu.Lock()
	s.compiling = true
	s.compilegr.Add(1)
	log.Println("[+] SSHD compiling")
	r := *s.r0

	// Pulling statistics from database 1
	if _, err := r.Do("SELECT", 1); err != nil {
		return err
	}

	// List days for which we need to update statistics
	toupdateD, err := redis.Strings(r.Do("SMEMBERS", "toupdate:daily"))
	if err != nil {
		return err
	}

	// Plot statistics for each day to update
	for _, v := range toupdateD {
		err = plotStats(s, v)
		if err != nil {
			return err
		}
		err = csvStats(s, v)
		if err != nil {
			return err
		}
	}

	// List months for which we need to update statistics
	toupdateM, err := redis.Strings(r.Do("SMEMBERS", "toupdate:monthly"))
	if err != nil {
		return err
	}

	// Plot statistics for each month to update
	for _, v := range toupdateM {
		err = plotStats(s, v)
		if err != nil {
			return err
		}
	}

	// List years for which we need to update statistics
	toupdateY, err := redis.Strings(r.Do("SMEMBERS", "toupdate:yearly"))
	if err != nil {
		return err
	}

	// Plot statistics for each year to update
	for _, v := range toupdateY {
		err = plotStats(s, v)
		if err != nil {
			return err
		}
	}

	// Get oldest / newest entries
	var newest string
	var oldest string
	if newest, err = redis.String(r.Do("GET", "newest")); err == redis.ErrNil {
		return err
	}
	if oldest, err = redis.String(r.Do("GET", "oldest")); err == redis.ErrNil {
		return err
	}
	parsedOldest, _ := time.Parse("20060102", oldest)
	parsedNewest, _ := time.Parse("20060102", newest)
	parsedOldestStr := parsedOldest.Format("2006-01-02")
	parsedNewestStr := parsedNewest.Format("2006-01-02")

	// Gettings list of years for which we have statistics
	reply, err := redis.Values(r.Do("SCAN", "0", "MATCH", "????:*", "COUNT", 1000))
	if err != nil {
		return err
	}
	var cursor int64
	var items []string
	_, err = redis.Scan(reply, &cursor, &items)
	if err != nil {
		return err
	}

	var years []string
	for _, v := range items {
		yearSplit := strings.Split(v, ":")
		found := false
		for _, y := range years {
			if y == yearSplit[0] {
				found = true
			}
		}
		if !found {
			years = append(years, yearSplit[0])
		}
	}

	// Gettings list of months for which we have statistics
	months := make(map[string][]string)
	for _, v := range years {
		var mraw []string
		reply, err = redis.Values(r.Do("SCAN", "0", "MATCH", v+"??:*", "COUNT", 1000))
		if err != nil {
			return err
		}

		_, err = redis.Scan(reply, &cursor, &mraw)
		if err != nil {
			return err
		}
		for _, m := range mraw {
			m = strings.TrimPrefix(m, v)
			monthSplit := strings.Split(m, ":")
			found := false
			for _, y := range months[v] {
				if y == monthSplit[0] {
					found = true
				}
			}
			if !found {
				months[v] = append(months[v], monthSplit[0])
			}
		}
	}

	// Parse Template
	t, err := template.ParseFiles(filepath.Join("logcompiler", "sshd", "statistics.gohtml"))
	if err != nil {
		return err
	}

	daily := struct {
		Title       string
		MinDate     string
		MaxDate     string
		CurrentTime string
	}{
		Title:       "sshd failed logins - daily statistics",
		MinDate:     parsedOldestStr,
		MaxDate:     parsedNewestStr,
		CurrentTime: parsedNewestStr,
	}

	monthly := struct {
		Title       string
		MonthList   map[string][]string
		CurrentTime string
	}{
		Title:       "sshd failed logins - monthly statistics",
		MonthList:   months,
		CurrentTime: parsedNewestStr,
	}

	yearly := struct {
		Title       string
		YearList    []string
		CurrentTime string
	}{
		Title:       "sshd failed logins - yearly statistics",
		YearList:    years,
		CurrentTime: parsedNewestStr,
	}

	// Create folder to store resulting files
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

	_ = os.Remove(filepath.Join("data", "sshd", "dailystatistics.html"))
	_ = os.Remove(filepath.Join("data", "sshd", "monthlystatistics.html"))
	_ = os.Remove(filepath.Join("data", "sshd", "yearlystatistics.html"))

	f, err := os.OpenFile(filepath.Join("data", "sshd", "dailystatistics.html"), os.O_RDWR|os.O_CREATE, 0666)
	defer f.Close()
	// err = t.Execute(f, daily)
	err = t.ExecuteTemplate(f, "headertpl", daily)
	err = t.ExecuteTemplate(f, "dailytpl", daily)
	err = t.ExecuteTemplate(f, "footertpl", daily)
	if err != nil {
		return err
	}

	f, err = os.OpenFile(filepath.Join("data", "sshd", "monthlystatistics.html"), os.O_RDWR|os.O_CREATE, 0666)
	defer f.Close()
	// err = t.Execute(f, monthly)
	err = t.ExecuteTemplate(f, "headertpl", monthly)
	err = t.ExecuteTemplate(f, "monthlytpl", monthly)
	err = t.ExecuteTemplate(f, "footertpl", monthly)
	if err != nil {
		return err
	}

	f, err = os.OpenFile(filepath.Join("data", "sshd", "yearlystatistics.html"), os.O_RDWR|os.O_CREATE, 0666)
	defer f.Close()
	// err = t.Execute(f, yearly)
	err = t.ExecuteTemplate(f, "headertpl", yearly)
	err = t.ExecuteTemplate(f, "yearlytpl", yearly)
	err = t.ExecuteTemplate(f, "footertpl", yearly)
	if err != nil {
		return err
	}

	// Copy js asset file
	input, err := ioutil.ReadFile(filepath.Join("logcompiler", "sshd", "load.js"))
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(filepath.Join("data", "sshd", "load.js"), input, 0644)
	if err != nil {
		return err
	}

	log.Println("[-] SSHD compiling finished.")
	s.compiling = false
	s.mu.Unlock()
	// Tell main program we can exit if needed now
	s.compilegr.Done()

	return nil
}

func csvStats(s *SSHDCompiler, v string) error {
	r := *s.r0
	zrank, err := redis.Strings(r.Do("ZRANGEBYSCORE", v, "-inf", "+inf", "WITHSCORES"))
	if err != nil {
		return err
	}

	stype := strings.Split(v, ":")

	// Create folder to store data
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

	var file *os.File
	if file, err = os.Create(filepath.Join("data", "sshd", stype[0], fmt.Sprintf("%v.csv", v))); err != nil {
		return err
	}

	defer file.Close()

	for k, v := range zrank {
		// pair: keys
		if (k % 2) == 0 {
			fmt.Fprintf(file, "%s, ", v)
			// even: values
		} else {
			fmt.Fprintln(file, v)
		}
	}

	return nil
}

func (s *SSHDCompiler) MISPexport() error {

	today := time.Now()
	dstr := fmt.Sprintf("%v%v%v", today.Year(), fmt.Sprintf("%02d", int(today.Month())), fmt.Sprintf("%02d", int(today.Day())))

	r0 := *s.r0
	r1 := *s.r1
	zrank, err := redis.Strings(r0.Do("ZRANGEBYSCORE", fmt.Sprintf("%q:statsusername", dstr), "-inf", "+inf", "WITHSCORES"))
	if err != nil {
		return err
	}

	mispobject := new(MISP_auth_failure_sshd_username)
	mispobject.mtype = "sshd"
	for k, v := range zrank {
		// pair: keys
		if (k % 2) == 0 {
			mispobject.username = v
			// even: values
		} else {
			mispobject.total = v
		}
	}

	b, err := json.Marshal(mispobject)
	r1.Do("LPUSH", "authf_object", b)

	return nil
}

func plotStats(s *SSHDCompiler, v string) error {
	r := *s.r0
	zrank, err := redis.Strings(r.Do("ZRANGEBYSCORE", v, "-inf", "+inf", "WITHSCORES"))
	if err != nil {
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
		return err
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
		return errors.New("we should not reach this point, open an issue")
	}

	p.Y.Label.Text = "Count"
	w := 0.5 * vg.Centimeter
	bc, err := plotter.NewBarChart(values, w)
	bc.Horizontal = true
	if err != nil {
		return err
	}
	bc.LineStyle.Width = vg.Length(0)
	bc.Color = plotutil.Color(2)

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
