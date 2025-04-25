package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fatih/color"
	"github.com/joho/godotenv"
	"github.com/mmcdole/gofeed"
)

// Constants
const (
	maxRetries          = 3
	retryBaseDelay      = time.Second
	checkWindowDays     = -10
	delayBetweenFeeds   = 10 * time.Second
	configFileName      = ".env"
	urlsFileName        = "data.txt"
	foundUrlsFileName   = "found-url.txt"
	lastCheckFileName   = "lastTimeCheck.txt"
	telegramAPITemplate = "https://api.telegram.org/bot%s/sendMessage"
)

// Configuration
var (
	keywords = map[string]string{
		"general":                        "0",
		"xss":                            "5",
		"open redirect":                  "12",
		"business logic":                 "11",
		"authentication":                 "10",
		"privilege escalation":           "9",
		"misconfiguration":               "8",
		"idor":                           "7",
		"access control":                 "6",
		"recon":                          "52",
		"osint":                          "51",
		"enumeration":                    "52",
		"fuzzing":                        "52",
		"bypass":                         "52",
		"cache poisoning":                "53",
		"Cache Deception":                "54",
		"HTTP Request Smuggling":         "55",
		"H2C Smuggling":                  "56",
		"Client Side Template Injection": "57",
		"Command Injection":              "58",
		"CRLF":                           "59",
		"Dangling Markup":                "60",
		"File Inclusion":                 "61",
		"Path Traversal":                 "61",
		"Prototype Pollution":            "62",
		"Server Side Inclusion":          "63",
		"Edge Side Inclusion":            "63",
		"Server Side Request Forgery":    "64",
		"Server Side Template Injection": "65",
		"Reverse Tab Nabbing":            "66",
		"XSLT Injection":                 "67",
		"XSSI":                           "68",
		"NoSQL":                          "69",
		"LDAP":                           "70",
		"ReDoS":                          "71",
		"SQL Injection":                  "2",
		"XPATH Injection":                "72",
		"Cross Site Request Forgery":     "74",
		"CSRF":                           "74",
		"Cross-site WebSocket hijacking": "75",
		"PostMessage Vulnerabilities":    "76",
		"Clickjacking":                   "77",
		"CSP bypass":                     "78",
		"2FA Bypass":                     "79",
		"Payment Bypass":                 "80",
		"Captcha Bypass":                 "81",
		"Login Bypass":                   "82",
		"Race Condition":                 "83",
		"Rate Limit":                     "84",
		"Reset Password":                 "85",
		"Mail Header Injection":          "86",
		"JWT":                            "87",
		"XXE":                            "88",
		"File Upload":                    "89",
		"OAUTH":                          "90",
		"SAML":                           "91",
		"Subdomain Takeover":             "92",
		"Parameter Pollution":            "93",
	}
)

// TelegramMessage represents the structure of a message to be sent to Telegram
type TelegramMessage struct {
	ChatID          string `json:"chat_id"`
	MessageThreadID string `json:"message_thread_id"`
	Text            string `json:"text"`
}

// Article represents a processed feed item
type Article struct {
	Title       string
	Description string
	Link        string
	Published   string
	Keywords    []string
}

// init loads environment variables from .env file
func init() {
	if err := godotenv.Load(configFileName); err != nil {
		log.Fatalf("Error loading %s file: %v", configFileName, err)
	}
}

func main() {
	printHeader("Starting Writeup Finder Script", color.FgGreen)

	// Configuration
	config := struct {
		MaxRetries        int
		BaseDelay         time.Duration
		Jitter            time.Duration
		MaxDelay          time.Duration
		CheckWindowDays   int
		DelayBetweenFeeds time.Duration
	}{
		MaxRetries:        3,
		BaseDelay:         2 * time.Second,
		Jitter:            1 * time.Second,
		MaxDelay:          30 * time.Second,
		CheckWindowDays:   -7, // Look back 7 days
		DelayBetweenFeeds: 5 * time.Second,
	}

	// Validate environment variables
	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	if botToken == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN environment variable not set")
	}
	channelID := os.Getenv("TELEGRAM_CHANNEL_ID")
	if channelID == "" {
		log.Fatal("TELEGRAM_CHANNEL_ID environment variable not set")
	}

	// Initialize tracking
	startTime := time.Now()
	headermsg := fmt.Sprintf("Writeup Finder Started - %s", startTime.Format("2006-01-02 15:04:05"))
	sendToTelegram(headermsg, botToken, channelID, keywords["general"])

	// Domain-specific rate limiter
	rateLimiter := NewRateLimiter(5*time.Second, 2*time.Second)

	// Load URLs
	urls, err := readURLs(urlsFileName)
	if err != nil {
		log.Fatalf("Error reading URLs: %v", err)
	}

	foundUrls, err := readFoundURLs(foundUrlsFileName)
	if err != nil {
		log.Printf("Warning: reading found URLs: %v", err)
		foundUrls = make(map[string]struct{})
	}

	cutoffTime := time.Now().AddDate(0, 0, config.CheckWindowDays)
	articlesFound := 0
	failedFeeds := 0

	// Process feeds
	for i, url := range urls {
		printStatus(fmt.Sprintf("Processing feed %d/%d: %s", i+1, len(urls), url), color.FgMagenta)

		// Respect domain rate limits
		domain := getDomain(url)
		rateLimiter.Wait(domain)

		// Fetch with retry and backoff
		articles, err := fetchArticlesWithRetry(url, config.MaxRetries, config.BaseDelay, config.Jitter, config.MaxDelay)
		if err != nil {
			printError(fmt.Sprintf("Error fetching feed from %s: %v", url, err))
			failedFeeds++
			continue
		}

		// Process articles
		newArticles := 0
		for _, item := range articles {
			if _, exists := foundUrls[item.Link]; exists {
				continue
			}

			article := processArticle(item)
			if article == nil {
				continue
			}

			pubDate, err := parseDate(item.Published)
			if err != nil {
				printError(fmt.Sprintf("Error parsing date for %s: %v", item.Link, err))
				continue
			}

			if pubDate.Before(cutoffTime) {
				continue
			}

			// Send notifications for each keyword
			for _, keyword := range article.Keywords {
				message := formatTelegramMessage(article, keyword)
				sendToTelegram(message, botToken, channelID, keywords[keyword])
				printSuccess(message)
				articlesFound++
				newArticles++
			}

			// Mark as processed
			if err := saveURL(item.Link, foundUrlsFileName); err != nil {
				printError(fmt.Sprintf("Error saving URL: %v", err))
				continue
			}
			foundUrls[item.Link] = struct{}{}
		}

		printStatus(fmt.Sprintf("Found %d new articles in this feed", newArticles), color.FgYellow)

		// Delay between feeds, but not after the last one
		if i < len(urls)-1 {
			time.Sleep(config.DelayBetweenFeeds + time.Duration(rand.Int63n(int64(config.Jitter))))
		}
	}

	// Final report
	duration := time.Since(startTime).Round(time.Second)
	finishedMsg := fmt.Sprintf("Completed in %s. Total new articles found: %d. Failed feeds: %d/%d",
		duration, articlesFound, failedFeeds, len(urls))

	printStatus(finishedMsg, color.FgCyan)
	printHeader("Writeup Hunter Script Completed", color.FgGreen)
	sendToTelegram(finishedMsg, botToken, channelID, keywords["general"])

	if err := updateLastCheckTime(lastCheckFileName); err != nil {
		printError(fmt.Sprintf("Error updating last check time: %v", err))
	}
}

// NewRateLimiter creates a domain-based rate limiter
type RateLimiter struct {
	mu       sync.Mutex
	lastReq  map[string]time.Time
	minDelay time.Duration
	jitter   time.Duration
}

func NewRateLimiter(minDelay, jitter time.Duration) *RateLimiter {
	return &RateLimiter{
		lastReq:  make(map[string]time.Time),
		minDelay: minDelay,
		jitter:   jitter,
	}
}

func (r *RateLimiter) Wait(domain string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if last, exists := r.lastReq[domain]; exists {
		elapsed := time.Since(last)
		if elapsed < r.minDelay {
			waitTime := r.minDelay - elapsed + time.Duration(rand.Int63n(int64(r.jitter)))
			time.Sleep(waitTime)
		}
	}
	r.lastReq[domain] = time.Now()
}

// fetchArticlesWithRetry implements exponential backoff
func fetchArticlesWithRetry(url string, maxRetries int, baseDelay, jitter, maxDelay time.Duration) (articles []*gofeed.Item, err error) {
	for attempt := 0; attempt <= maxRetries; attempt++ {
		articles, err = fetchArticles(url)
		if err == nil {
			return articles, nil
		}

		if shouldRetry(err) {
			delay := getBackoffDelay(attempt, baseDelay, jitter, maxDelay)
			time.Sleep(delay)
			continue
		}
		break
	}
	return nil, fmt.Errorf("after %d attempts: %w", maxRetries, err)
}

func shouldRetry(err error) bool {
	if err == nil {
		return false
	}

	// Handle HTTP errors
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		// Retry on 5xx server errors and 429 (Too Many Requests)
		if httpErr.StatusCode >= 500 || httpErr.StatusCode == http.StatusTooManyRequests {
			return true
		}
		// Don't retry on client errors (4xx) except 429
		if httpErr.StatusCode >= 400 && httpErr.StatusCode < 500 {
			return false
		}
	}

	// Handle network errors (timeouts, connection resets, etc.)
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() || netErr.Temporary() {
			return true
		}
	}

	// Handle DNS errors
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return !dnsErr.IsNotFound
	}

	// Handle URL errors (malformed URLs, etc.)
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		// Only retry if it's a timeout or temporary error
		if urlErr.Timeout() || urlErr.Temporary() {
			return true
		}
	}

	// Handle specific error cases
	switch {
	case errors.Is(err, io.EOF):
		return true // Server closed connection
	case errors.Is(err, syscall.ECONNREFUSED):
		return true // Connection refused
	case errors.Is(err, syscall.ECONNRESET):
		return true // Connection reset by peer
	case strings.Contains(err.Error(), "TLS handshake timeout"):
		return true
	}

	// Default case - don't retry on unknown errors
	return false
}

type HTTPError struct {
	StatusCode int
	Body       []byte
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP error %d: %s", e.StatusCode, string(e.Body))
}

func getBackoffDelay(attempt int, baseDelay, jitter, maxDelay time.Duration) time.Duration {
	delay := baseDelay * time.Duration(math.Pow(2, float64(attempt)))
	delay += time.Duration(rand.Int63n(int64(jitter)))

	if delay > maxDelay {
		return maxDelay
	}
	return delay
}

func getDomain(urlStr string) string {
	u, err := url.Parse(urlStr)
	if err != nil {
		return "default"
	}
	return u.Hostname()
}

// Helper functions

func printHeader(message string, colorAttr color.Attribute) {
	colored := color.New(colorAttr).SprintFunc()
	fmt.Println(colored(strings.Repeat("=", 80)))
	fmt.Println(colored(fmt.Sprintf("%80s", message)))
	fmt.Println(colored(strings.Repeat("=", 80)))
}

func printStatus(message string, colorAttr color.Attribute) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	colored := color.New(colorAttr).SprintFunc()
	fmt.Println(color.CyanString(timestamp), "-", colored(message))
}

func printError(message string) {
	fmt.Println(color.RedString("ERROR: %s", message))
}

func printSuccess(message string) {
	fmt.Println(color.GreenString(message))
	fmt.Println()
}

func readURLs(filename string) ([]string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", filename, err)
	}
	defer file.Close()

	var urls []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if url := strings.TrimSpace(scanner.Text()); url != "" {
			urls = append(urls, url)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanning %s: %w", filename, err)
	}

	return urls, nil
}

func readFoundURLs(filename string) (map[string]struct{}, error) {
	foundUrls := make(map[string]struct{})

	file, err := os.Open(filename)
	if os.IsNotExist(err) {
		return foundUrls, nil
	}
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", filename, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		foundUrls[scanner.Text()] = struct{}{}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanning %s: %w", filename, err)
	}

	return foundUrls, nil
}

func saveURL(url, filename string) error {
	file, err := os.OpenFile(filename, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return fmt.Errorf("opening %s: %w", filename, err)
	}
	defer file.Close()

	if _, err := file.WriteString(url + "\n"); err != nil {
		return fmt.Errorf("writing to %s: %w", filename, err)
	}

	return nil
}

func updateLastCheckTime(filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("creating %s: %w", filename, err)
	}
	defer file.Close()

	currentTime := time.Now().Format(time.RFC3339)
	if _, err := file.WriteString(currentTime); err != nil {
		return fmt.Errorf("writing to %s: %w", filename, err)
	}

	return nil
}

func fetchArticles(feedURL string) ([]*gofeed.Item, error) {
	fp := gofeed.NewParser()

	// Check if it's our specific JSON feed
	if strings.Contains(feedURL, "writeups.xyz/index.json") {
		return parseWriteupsXYZFeed(feedURL)
	}

	// Handle regular RSS/Atom feeds
	feed, err := fp.ParseURL(feedURL)
	if err != nil {
		return nil, fmt.Errorf("parsing feed URL: %w", err)
	}
	return feed.Items, nil
}

func parseWriteupsXYZFeed(feedURL string) ([]*gofeed.Item, error) {
	resp, err := http.Get(feedURL)
	if err != nil {
		return nil, fmt.Errorf("fetching JSON feed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	// Define a struct that matches the JSON structure
	type jsonItem struct {
		Title         string `json:"title"`
		Description   string `json:"description"`
		Link          string `json:"link"`
		PublishedDate string `json:"published"`
		Authors       []struct {
			Name string `json:"name"`
		} `json:"authors"`
		Vulnerabilities []struct {
			Title string `json:"title"`
		} `json:"vulnerabilities"`
	}

	var items []jsonItem
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("unmarshaling JSON: %w", err)
	}

	// Convert to gofeed.Items
	var feedItems []*gofeed.Item
	for _, item := range items {
		// Format authors
		var authors []string
		for _, author := range item.Authors {
			authors = append(authors, author.Name)
		}

		// Format vulnerabilities/tags
		var tags []string
		for _, vuln := range item.Vulnerabilities {
			tags = append(tags, vuln.Title)
		}

		feedItem := &gofeed.Item{
			Title:       item.Title,
			Description: item.Description,
			Link:        item.Link,
			Published:   item.PublishedDate,
			// Custom fields can be added to the Extensions map if needed
		}

		// // If you need to preserve the authors and tags, you could add them to a custom field
		// if len(authors) > 0 {
		// 	if feedItem.Extensions == nil {
		// 		feedItem.Extensions = make(map[string]map[string][]gofeed.Extension)
		// 	}
		// 	feedItem.Extensions["custom"] = map[string][]gofeed.Extension{
		// 		"authors": {gofeed.Extension{Value: strings.Join(authors, ", ")}},
		// 	}

		feedItems = append(feedItems, feedItem)
		// }
	}
	return feedItems, nil
}

// func fetchArticlesWithRetry(feedURL string, maxRetries int) ([]*gofeed.Item, error) {
// 	var lastErr error

// 	for i := range maxRetries {
// 		articles, err := fetchArticles(feedURL)
// 		if err == nil {
// 			return articles, nil
// 		}

// 		if strings.Contains(err.Error(), "429") {
// 			waitTime := time.Duration(math.Pow(2, float64(i))) * retryBaseDelay
// 			time.Sleep(waitTime)
// 			lastErr = err
// 			continue
// 		}

// 		return nil, fmt.Errorf("fetching articles: %w", err)
// 	}

// 	return nil, fmt.Errorf("after %d retries: %w", maxRetries, lastErr)
// }

func parseDate(dateStr string) (time.Time, error) {
	// Try multiple common date formats
	formats := []string{
		time.RFC1123,  // "Mon, 02 Jan 2006 15:04:05 MST"
		time.RFC1123Z, // "Mon, 02 Jan 2006 15:04:05 -0700"
		time.RFC3339,  // "2006-01-02T15:04:05Z07:00"
		"2006-01-02 15:04:05 -0700",
		"2006-01-02 15:04:05",
		"02 Jan 2006 15:04:05 MST",
	}

	for _, format := range formats {
		t, err := time.Parse(format, dateStr)
		if err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("unable to parse date: %s", dateStr)
}

func processArticle(item *gofeed.Item) *Article {
	articleText := strings.ToLower(item.Title + " " + item.Description)
	var matchedKeywords []string

	for keyword := range keywords {
		if strings.Contains(articleText, strings.ToLower(keyword)) {
			matchedKeywords = append(matchedKeywords, keyword)
		}
	}

	if len(matchedKeywords) == 0 {
		return nil
	}

	return &Article{
		Title:       item.Title,
		Description: item.Description,
		Link:        item.Link,
		Published:   item.Published,
		Keywords:    matchedKeywords,
	}
}

func formatTelegramMessage(article *Article, keyword string) string {
	cleanedLink := cleanURL(article.Link)

	if strings.Contains(cleanedLink, "medium.com") {
		cleanedLink = fmt.Sprintf("https://freedium.cfd/%s", cleanedLink)
	}

	return fmt.Sprintf("▶ %s\nPublished: %s\nLink: %s\nTags: %s",
		article.Title, article.Published, cleanedLink, keyword)
}

// cleanURL removes tracking parameters (e.g., ?source=...) from URLs
func cleanURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL // Return original if parsing fails
	}

	// Remove unwanted query parameters (e.g., "source", "utm_*")
	query := parsed.Query()
	for param := range query {
		if param == "source" || strings.HasPrefix(param, "utm_") {
			query.Del(param)
		}
	}
	parsed.RawQuery = query.Encode()

	return parsed.String()
}

func sendToTelegram(message, botToken, channelID, messageThreadID string) {
	url := fmt.Sprintf(telegramAPITemplate, botToken)

	telegramMessage := TelegramMessage{
		ChatID:          channelID + "_" + messageThreadID,
		Text:            message,
		MessageThreadID: messageThreadID,
	}

	jsonData, err := json.Marshal(telegramMessage)
	if err != nil {
		printError(fmt.Sprintf("marshalling Telegram message: %v", err))
		return
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		printError(fmt.Sprintf("sending message to Telegram: %v", err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		printError(fmt.Sprintf("Telegram API responded with status: %d", resp.StatusCode))
	}
}
