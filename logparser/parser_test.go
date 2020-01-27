package logparser

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"regexp"
	"testing"
)

var expected = map[int]map[string]string{
	0: map[string]string{
		"date":     "Jan 22 11:59:37",
		"host":     "sigmund",
		"username": "git",
		"src":      "106.12.14.144",
	},
	1: map[string]string{
		"date":     "Jan 22 11:37:19",
		"host":     "si.mund",
		"username": "gestion",
		"src":      "159.89.153.54",
	},
	2: map[string]string{
		"date":     "Jan 22 11:34:46",
		"host":     "sigmund",
		"username": "atpco",
		"src":      "177.152.124.21",
	},
	3: map[string]string{
		"date":     "Jan 22 11:33:07",
		"host":     "sigmund",
		"username": "ki",
		"src":      "49.233.183.158",
	},
	4: map[string]string{
		"date":     "Jan 22 11:29:16",
		"host":     "sigmund",
		"username": "a.min",
		"src":      "185.56.8.191",
	},
}

func TestSshdParser(t *testing.T) {
	// Opening sshd test file
	fmt.Println("[+] Testing the sshd log parser")
	f, err := os.Open("./test.log")
	if err != nil {
		log.Fatalf("Error opening test file: %v", err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	c := 0
	for scanner.Scan() {
		re := regexp.MustCompile(`^(?P<date>[[:alpha:]]{3}\s\d{2}\s\d{2}:\d{2}:\d{2}) (?P<host>[^ ]+) sshd\[[[:alnum:]]+\]: Invalid user (?P<username>[^ ]+) from (?P<src>.*$)`)
		n1 := re.SubexpNames()
		r2 := re.FindAllStringSubmatch(scanner.Text(), -1)[0]

		// Build the group map for the line
		md := map[string]string{}
		for i, n := range r2 {
			// fmt.Printf("%d. match='%s'\tname='%s'\n", i, n, n1[i])
			md[n1[i]] = n
		}

		// Check against the expected map
		for _, n := range n1 {
			if n != "" {
				if md[n] != expected[c][n] {
					t.Errorf("%v = '%v'; want '%v'", n, md[n], expected[c][n])
				}
			}
		}
		c++
	}
}
