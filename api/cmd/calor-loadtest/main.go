// Command calor-loadtest drives concurrent ingest batches and dashboard
// queries against a running API and reports throughput, latency and (with
// -pid) the API process's resident memory sampled from /proc; -pg adds the
// memory of the local PostgreSQL server.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type stats struct {
	mu   sync.Mutex
	lat  []time.Duration
	errs int
	n    int
}

func (s *stats) add(d time.Duration, ok bool) {
	s.mu.Lock()
	s.lat = append(s.lat, d)
	s.n++
	if !ok {
		s.errs++
	}
	s.mu.Unlock()
}

func (s *stats) pct(p float64) time.Duration {
	if len(s.lat) == 0 {
		return 0
	}
	sort.Slice(s.lat, func(i, j int) bool { return s.lat[i] < s.lat[j] })
	return s.lat[int(float64(len(s.lat)-1)*p)]
}

func rssKB(pid int, field string) int {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, field+":") {
			f := strings.Fields(line)
			if len(f) >= 2 {
				n, _ := strconv.Atoi(f[1])
				return n
			}
		}
	}
	return 0
}

// pgMemKB sums the memory of every local postgres process (postmaster,
// backends, background workers). PSS splits shared buffers between the
// processes that map them, so the sum is not inflated by shared memory;
// reading it needs the postgres user or root, otherwise RSS is summed.
func pgMemKB() (kb int, field string) {
	dirs, _ := os.ReadDir("/proc")
	pss, rss := 0, 0
	pssOK := true
	for _, d := range dirs {
		pid, err := strconv.Atoi(d.Name())
		if err != nil {
			continue
		}
		comm, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
		if err != nil || strings.TrimSpace(string(comm)) != "postgres" {
			continue
		}
		rss += rssKB(pid, "VmRSS")
		if b, err := os.ReadFile(fmt.Sprintf("/proc/%d/smaps_rollup", pid)); err == nil {
			for _, line := range strings.Split(string(b), "\n") {
				if f := strings.Fields(line); len(f) >= 2 && f[0] == "Pss:" {
					n, _ := strconv.Atoi(f[1])
					pss += n
				}
			}
		} else {
			pssOK = false
		}
	}
	if pssOK && pss > 0 {
		return pss, "PSS"
	}
	return rss, "RSS"
}

func main() {
	api := flag.String("api", "http://127.0.0.1:8080", "API base URL")
	key := flag.String("key", os.Getenv("CALOR_ADMIN_KEY"), "admin key")
	dur := flag.Duration("duration", 30*time.Second, "test duration")
	ingestC := flag.Int("ingest", 16, "concurrent ingest workers")
	queryC := flag.Int("query", 4, "concurrent query workers")
	batch := flag.Int("batch", 20, "events per ingest request")
	pages := flag.Int("pages", 40, "distinct pages")
	pid := flag.Int("pid", 0, "API pid to sample RSS from /proc")
	keep := flag.Bool("keep", false, "keep the loadtest site and its events afterwards")
	pgMem := flag.Bool("pg", false, "also sample the summed memory of local postgres processes")
	flag.Parse()

	client := &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{MaxIdleConnsPerHost: 64}}
	site := "loadtest"
	deleteSite := func() {
		req, _ := http.NewRequest("DELETE", *api+"/api/sites/"+site, nil)
		req.Header.Set("Authorization", "Bearer "+*key)
		if res, err := client.Do(req); err == nil {
			res.Body.Close()
		}
	}
	// Start from an empty site so every run measures the same thing.
	deleteSite()
	body, _ := json.Marshal(map[string]string{"id": site, "name": "Load test", "domain": "loadtest.local"})
	req, _ := http.NewRequest("POST", *api+"/api/sites", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+*key)
	if res, err := client.Do(req); err != nil {
		fmt.Fprintln(os.Stderr, "register site:", err)
		os.Exit(1)
	} else {
		res.Body.Close()
	}

	var ing, qry stats
	var events atomic.Int64
	var throttled atomic.Int64
	var maxRSS, startRSS, maxPG, startPG int
	var pgField string
	if *pid > 0 {
		startRSS = rssKB(*pid, "VmRSS")
	}
	if *pgMem {
		startPG, pgField = pgMemKB()
	}
	stop := make(chan struct{})
	var wg sync.WaitGroup

	if *pid > 0 || *pgMem {
		wg.Add(1)
		go func() {
			defer wg.Done()
			t := time.NewTicker(250 * time.Millisecond)
			defer t.Stop()
			for {
				select {
				case <-stop:
					return
				case <-t.C:
					if v := rssKB(*pid, "VmRSS"); *pid > 0 && v > maxRSS {
						maxRSS = v
					}
					if *pgMem {
						if v, _ := pgMemKB(); v > maxPG {
							maxPG = v
						}
					}
				}
			}
		}()
	}

	for w := 0; w < *ingestC; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			r := rand.New(rand.NewSource(int64(w)))
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				page := fmt.Sprintf("https://loadtest.local/p/%d", r.Intn(*pages))
				var sb strings.Builder
				fmt.Fprintf(&sb, `{"v":1,"site":%q,"sid":"w%ds%d","ts":%d,"u":%q,"p":"pv%d_%d","vw":1440,"vh":900,"e":[`, site, w, i/10, time.Now().UnixMilli(), page, w, i)
				for j := 0; j < *batch; j++ {
					if j > 0 {
						sb.WriteByte(',')
					}
					switch {
					case j == 0:
						sb.WriteString(`{"t":"p","k":"load","sd":40}`)
					case j == *batch-1:
						fmt.Fprintf(&sb, `{"t":"s","sd":%d,"ms":%d}`, 40+r.Intn(61), 1000+r.Intn(60000))
					default:
						fmt.Fprintf(&sb, `{"t":"c","d":-%d,"x":%d,"y":%d,"dw":1440,"dh":3200,"el":{"sel":"main>section:nth-of-type(%d)>a.btn","tag":"a","txt":"Button %d"}}`,
							r.Intn(5000), r.Intn(1440), r.Intn(3200), r.Intn(6)+1, r.Intn(12))
					}
				}
				sb.WriteString("]}")
				req, _ := http.NewRequest("POST", *api+"/api/events", strings.NewReader(sb.String()))
				req.Header.Set("Content-Type", "text/plain;charset=UTF-8")
				req.Header.Set("Origin", "https://loadtest.local")
				t0 := time.Now()
				res, err := client.Do(req)
				if err == nil && res.StatusCode == http.StatusServiceUnavailable {
					// Queue full: back off like a well-behaved client instead of spinning.
					io.Copy(io.Discard, res.Body)
					res.Body.Close()
					throttled.Add(1)
					time.Sleep(100 * time.Millisecond)
					continue
				}
				ok := err == nil && res.StatusCode == http.StatusAccepted
				if err == nil {
					io.Copy(io.Discard, res.Body)
					res.Body.Close()
				}
				ing.add(time.Since(t0), ok)
				if ok {
					events.Add(int64(*batch))
				}
			}
		}(w)
	}

	queries := []string{"kpis", "pages?sort=clicks", "elements", "time-of-day", "scroll", "daily", "entry-exit", "heatmap?url=https://loadtest.local/p/1&cols=100&rows=220", "heatmap?url=https://loadtest.local/p/2&cols=100&rows=220&from=" + strconv.FormatInt(time.Now().Add(-time.Hour).UnixMilli()+1, 10)}
	for w := 0; w < *queryC; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := w; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				q := queries[i%len(queries)]
				sep := "?"
				if strings.Contains(q, "?") {
					sep = "&"
				}
				// Unique cache-busting parameter so every query hits the database.
				req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/sites/%s/%s%stz=%d", *api, site, q, sep, i%600), nil)
				req.Header.Set("Authorization", "Bearer "+*key)
				req.Header.Set("Accept-Encoding", "gzip")
				t0 := time.Now()
				res, err := client.Do(req)
				ok := err == nil && res.StatusCode == http.StatusOK
				if err == nil {
					io.Copy(io.Discard, res.Body)
					res.Body.Close()
				}
				qry.add(time.Since(t0), ok)
			}
		}(w)
	}

	time.Sleep(*dur)
	close(stop)
	wg.Wait()
	secs := dur.Seconds()
	fmt.Printf("duration            %s (ingest workers %d, query workers %d, %d events/request)\n", *dur, *ingestC, *queryC, *batch)
	fmt.Printf("ingest requests     %d (%.0f req/s, %.0f events/s, %d errors, %d throttled with 503)  p50 %s  p95 %s  p99 %s\n",
		ing.n, float64(ing.n)/secs, float64(events.Load())/secs, ing.errs, throttled.Load(), ing.pct(.5), ing.pct(.95), ing.pct(.99))
	fmt.Printf("query requests      %d (%.1f req/s, %d errors)  p50 %s  p95 %s  p99 %s\n",
		qry.n, float64(qry.n)/secs, qry.errs, qry.pct(.5), qry.pct(.95), qry.pct(.99))
	if !*keep {
		defer deleteSite()
	}
	if *pid > 0 {
		fmt.Printf("api RSS             start %.1f MiB, peak sampled %.1f MiB, VmHWM %.1f MiB, end %.1f MiB\n",
			float64(startRSS)/1024, float64(maxRSS)/1024, float64(rssKB(*pid, "VmHWM"))/1024, float64(rssKB(*pid, "VmRSS"))/1024)
	}
	if *pgMem {
		end, _ := pgMemKB()
		fmt.Printf("postgres %s sum    start %.1f MiB, peak sampled %.1f MiB, end %.1f MiB\n",
			pgField, float64(startPG)/1024, float64(maxPG)/1024, float64(end)/1024)
	}
}
