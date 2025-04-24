package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"strings"
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

	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	channelID := os.Getenv("TELEGRAM_CHANNEL_ID")

	headermsg := fmt.Sprintf("Writeup Finder Started - %s", time.Now().Format("2006-01-02"))

	sendToTelegram(headermsg, botToken, channelID, keywords["general"])

	urls, err := readURLs(urlsFileName)
	if err != nil {
		log.Fatalf("Error reading URLs: %v", err)
	}

	foundUrls, err := readFoundURLs(foundUrlsFileName)
	if err != nil {
		log.Printf("Warning: %v", err)
	}

	cutoffTime := time.Now().AddDate(0, 0, checkWindowDays)
	articlesFound := 0

	for i, url := range urls {
		printStatus(fmt.Sprintf("Processing feed: %s", url), color.FgMagenta)

		articles, err := fetchArticlesWithRetry(url, maxRetries)
		if err != nil {
			printError(fmt.Sprintf("Error fetching feed from %s: %v", url, err))
			continue
		}

		for _, item := range articles {
			if _, exists := foundUrls[item.Link]; exists {
				continue
			}

			article := processArticle(item)
			if article == nil {
				continue
			}

			pubDate, err := parseDate(item.Published)
			if err != nil || pubDate.Before(cutoffTime) {
				if err := saveURL(item.Link, foundUrlsFileName); err != nil {
					printError(fmt.Sprintf("Error saving URL: %v", err))
					continue
				}

				for _, keyword := range article.Keywords {
					message := formatTelegramMessage(article, keyword)
					sendToTelegram(message, botToken, channelID, keywords[keyword])
					printSuccess(message)
					articlesFound++
				}
			}
		}

		if i < len(urls)-1 {
			time.Sleep(delayBetweenFeeds)
		}
	}

	finisedMsg := fmt.Sprintf("Total new articles found: %d", articlesFound)
	printStatus(finisedMsg, color.FgCyan)
	printHeader("Writeup Hunter Script Completed", color.FgGreen)
	sendToTelegram(finisedMsg, botToken, channelID, keywords["general"])

	if err := updateLastCheckTime(lastCheckFileName); err != nil {
		printError(fmt.Sprintf("Error updating last check time: %v", err))
	}
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
	feed, err := fp.ParseURL(feedURL)
	if err != nil {
		return nil, err
	}
	return feed.Items, nil
}

func fetchArticlesWithRetry(feedURL string, maxRetries int) ([]*gofeed.Item, error) {
	var lastErr error

	for i := 0; i < maxRetries; i++ {
		articles, err := fetchArticles(feedURL)
		if err == nil {
			return articles, nil
		}

		if strings.Contains(err.Error(), "429") {
			waitTime := time.Duration(math.Pow(2, float64(i))) * retryBaseDelay
			time.Sleep(waitTime)
			lastErr = err
			continue
		}

		return nil, fmt.Errorf("fetching articles: %w", err)
	}

	return nil, fmt.Errorf("after %d retries: %w", maxRetries, lastErr)
}

func parseDate(dateString string) (time.Time, error) {
	formats := []string{time.RFC1123Z, time.RFC1123}
	var lastErr error

	for _, format := range formats {
		t, err := time.Parse(format, dateString)
		if err == nil {
			return t, nil
		}
		lastErr = err
	}

	return time.Time{}, fmt.Errorf("parsing date: %w", lastErr)
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
	if strings.Contains(article.Link, "medium.com") {
		article.Link = fmt.Sprintf("https://freedium.cfd/%s", article.Link)
	}
	return fmt.Sprintf("▶ %s\nPublished: %s\nLink: %s\nTags: %s",
		article.Title, article.Published, article.Link, keyword)
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
