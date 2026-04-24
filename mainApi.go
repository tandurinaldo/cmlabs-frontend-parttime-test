package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/gin-gonic/gin"
)

var maxWorker = 3

type Job struct {
	Status        string
	CmlabsCount   int
	SequenceCount int
}

var jobs = make(map[string]*Job)
var mu sync.Mutex

func main() {
	r := gin.Default()

	r.GET("/crawl-multi", func(c *gin.Context) {
		urls := []string{
			"https://cmlabs.co/en-id",
			"https://sequence.day",
			"https://elektro.widyakartika.ac.id/",
		}

		jobID := fmt.Sprintf("%d", time.Now().UnixNano())

		mu.Lock()
		jobs[jobID] = &Job{
			Status: "running",
		}
		mu.Unlock()

		go crawlMultiple(jobID, urls)

		c.JSON(http.StatusOK, gin.H{
			"job_id":     jobID,
			"status":     "started",
			"result_url": "http://localhost:8080/result?id=" + jobID,
		})
	})

	r.GET("/result", func(c *gin.Context) {
		id := c.Query("id")

		mu.Lock()
		job, exists := jobs[id]

		log.Println("Requested ID:", id)
		log.Println("Available Jobs:", jobs)

		mu.Unlock()

		if !exists {
			c.JSON(404, gin.H{
				"error": "job not found",
				"hint":  "pastikan job_id benar atau server belum direstart",
			})
			return
		}

		c.JSON(200, gin.H{
			"status":         job.Status,
			"cmlabs_count":   job.CmlabsCount,
			"sequence_count": job.SequenceCount,
		})
	})

	r.Run(":8080")
}

// ===== MULTI CRAWLER =====
func crawlMultiple(jobID string, urls []string) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxWorker)

	for _, u := range urls {
		wg.Add(1)

		go func(startURL string) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			crawlAll(jobID, startURL)
		}(u)
	}

	wg.Wait()

	mu.Lock()
	jobs[jobID].Status = "finished"
	mu.Unlock()

	log.Println("✅Crawling Complete for Job:", jobID)
}

func crawlAll(jobID, start string) {
	visited := make(map[string]bool)
	queue := []string{start}

	base, _ := url.Parse(start)

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if visited[current] {
			continue
		}
		visited[current] = true

		log.Println("Crawling:", current)

		html, links, err := crawlPage(current)
		if err != nil {
			log.Println("Error:", err)
			continue
		}

		saveHTML(start, current, html)

		for _, link := range links {
			u, err := url.Parse(link)
			if err != nil {
				continue
			}

			full := u.String()

			mu.Lock()

			if strings.Contains(u.Host, "cmlabs.co") {
				if strings.Contains(full, "collaboration") || strings.Contains(full, "team") {
					jobs[jobID].CmlabsCount++
				}
			}

			if strings.Contains(u.Host, "sequence.day") {
				if strings.Contains(full, "industry-leaders") {
					jobs[jobID].SequenceCount++
				}
			}

			mu.Unlock()

			if u.Host == base.Host {
				if !visited[full] {
					queue = append(queue, full)
				}
			}
		}

		time.Sleep(1 * time.Second)
	}
}

func crawlPage(target string) (string, []string, error) {
	ctx, cancel := chromedp.NewContext(context.Background())
	defer cancel()

	ctx, cancel = context.WithTimeout(ctx, 40*time.Second)
	defer cancel()

	var html string
	var links []string

	err := chromedp.Run(ctx,
		chromedp.Navigate(target),
		chromedp.WaitVisible("body"),
		chromedp.Sleep(2*time.Second),
		chromedp.OuterHTML("html", &html),
		chromedp.Evaluate(`
			Array.from(document.querySelectorAll('a')).map(a => a.href)
		`, &links),
	)

	return html, links, err
}

func saveHTML(baseURL, pageURL, html string) {
	base, _ := url.Parse(baseURL)
	u, _ := url.Parse(pageURL)

	path := u.Path
	if path == "" || path == "/" {
		path = "/index"
	}

	dir := "output/" + base.Host
	filePath := dir + path + ".html"

	os.MkdirAll(filepath.Dir(filePath), os.ModePerm)
	os.WriteFile(filePath, []byte(html), 0644)
}
