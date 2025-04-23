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

type TelegramMessage struct {
	ChatID string `json:"chat_id"`
	Text   string `json:"text"`
}

type KeywordFilter struct {
	Include []string
	Exclude []string
}

func init() {
	err := godotenv.Load()
	if err != nil {
		log.Fatalf("Error loading .env file")
	}
}

func updateLastTimeCheck() {
	file, err := os.Create("lastTimeCheck.txt")
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	currentTime := time.Now().Format(time.RFC3339)
	_, err = file.WriteString(currentTime)
	if err != nil {
		log.Fatal(err)
	}
	file.WriteString(currentTime)
}

func readUrls() []string {
	file, err := os.Open("data.txt")
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	var urls []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			urls = append(urls, line)
		}
	}
	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}
	return urls
}

func readFoundUrls() map[string]struct{} {
	foundUrls := make(map[string]struct{})
	file, err := os.Open("found-url.txt")
	if err != nil {
		return foundUrls
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		foundUrls[scanner.Text()] = struct{}{}
	}

	if err := scanner.Err(); err != nil {
		fmt.Println(color.RedString("Error scanning found URL file: %s", err))
	}
	return foundUrls
}

func saveUrl(url string) {
	file, err := os.OpenFile("found-url.txt", os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		fmt.Println(color.RedString("Error opening found URL file: %s", err))
		return
	}
	defer file.Close()

	file.WriteString(url + "\n")
}

func readKeywords() KeywordFilter {
	file, err := os.Open("keywords.txt")
	if err != nil {
		fmt.Println(color.YellowString("No keywords.txt file found - will process all articles"))
		return KeywordFilter{}
	}
	defer file.Close()

	var filter KeywordFilter
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		keyword := strings.TrimSpace(scanner.Text())
		if keyword == "" {
			continue
		}

		if strings.HasPrefix(keyword, "-") {
			filter.Exclude = append(filter.Exclude, strings.ToLower(keyword[1:]))
		} else {
			cleanKeyword := strings.TrimPrefix(keyword, "+")
			filter.Include = append(filter.Include, strings.ToLower(cleanKeyword))
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Println(color.RedString("Error reading keywords: %s", err))
	}
	return filter
}

func matchesFilter(text string, filter KeywordFilter) bool {
	lowerText := strings.ToLower(text)

	// Check exclude keywords first
	for _, keyword := range filter.Exclude {
		if strings.Contains(lowerText, keyword) {
			return false
		}
	}

	// If no include keywords, return true
	if len(filter.Include) == 0 {
		return true
	}

	// Check include keywords
	for _, keyword := range filter.Include {
		if strings.Contains(lowerText, keyword) {
			return true
		}
	}

	return false
}

func fetchArticles(feedUrl string) ([]*gofeed.Item, error) {
	fp := gofeed.NewParser()
	feed, err := fp.ParseURL(feedUrl)
	if err != nil {
		return nil, err
	}
	return feed.Items, nil
}

func fetchArticlesWithRetry(feedUrl string, maxRetries int) ([]*gofeed.Item, error) {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		articles, err := fetchArticles(feedUrl)
		if err == nil {
			return articles, nil
		}

		if strings.Contains(err.Error(), "429") {
			waitTime := time.Duration(math.Pow(2, float64(i))) * time.Second
			time.Sleep(waitTime)
			lastErr = err
			continue
		}

		return nil, err
	}
	return nil, fmt.Errorf("after %d retries: %v", maxRetries, lastErr)
}

func parseDate(dateString string) (time.Time, error) {
	t, err := time.Parse(time.RFC1123Z, dateString)
	if err != nil {
		t, err = time.Parse(time.RFC1123, dateString)
	}
	return t, err
}

func printPretty(message string, colorAttr color.Attribute, isTitle bool) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	colored := color.New(colorAttr).SprintFunc()

	if isTitle {
		fmt.Println(colored(strings.Repeat("=", 80)))
		fmt.Println(colored(fmt.Sprintf("%80s", message)))
		fmt.Println(colored(strings.Repeat("=", 80)))
	} else {
		fmt.Println(color.CyanString(timestamp), "-", colored(message))
	}
}

func sendToTelegram(message string, botToken string, channelID string) {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)

	telegramMessage := TelegramMessage{
		ChatID: channelID,
		Text:   message,
	}

	jsonData, err := json.Marshal(telegramMessage)
	if err != nil {
		fmt.Println(color.RedString("Error marshalling Telegram message: %s", err))
		return
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		fmt.Println(color.RedString("Error sending message to Telegram: %s", err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Println(color.RedString("Failed to send message to Telegram, status code: %d", resp.StatusCode))
	}
}

func main() {
	TELEGRAM_BOT_TOKEN := os.Getenv("TELEGRAM_BOT_TOKEN")
	TELEGRAM_CHANNEL_ID := os.Getenv("TELEGRAM_CHANNEL_ID")
	printPretty("Starting Writeup Finder Script", color.FgGreen, true)

	urls := readUrls()
	keywords := readKeywords()
	foundUrls := readFoundUrls()
	tenDaysAgo := time.Now().AddDate(0, 0, -10)

	articlesFound := 0

	for i, url := range urls {
		printPretty(fmt.Sprintf("Processing feed: %s", url), color.FgMagenta, false)
		articles, err := fetchArticlesWithRetry(url, 3)
		if err != nil {
			fmt.Println(color.RedString("Error fetching feed from %s: %s", url, err))
			continue
		}

		for _, article := range articles {
			// Skip if we've already processed this URL
			if _, exists := foundUrls[article.Link]; exists {
				continue
			}

			// Combine title and description for keyword matching
			articleText := article.Title
			if article.Description != "" {
				articleText += " " + article.Description
			}

			// Check if article matches our keyword filter
			if !matchesFilter(articleText, keywords) {
				continue
			}

			pubDate, err := parseDate(article.Published)
			if err != nil || pubDate.Before(tenDaysAgo) {
				saveUrl(article.Link)

				// Include matched keywords in the message
				matchedKeywords := findMatchedKeywords(articleText, keywords)
				message := fmt.Sprintf("▶ %s\nPublished: %s\nLink: %s\nTags: %v",
					article.Title, article.Published, article.Link, matchedKeywords)

				sendToTelegram(message, TELEGRAM_BOT_TOKEN, TELEGRAM_CHANNEL_ID)

				fmt.Println(color.GreenString(message))
				fmt.Println()
				articlesFound++
			}
		}

		if i < len(urls)-1 {
			time.Sleep(10 * time.Second)
		}
	}

	printPretty(fmt.Sprintf("Total new articles found: %d", articlesFound), color.FgCyan, false)
	printPretty("Writeup Hunter Script Completed", color.FgGreen, true)
	updateLastTimeCheck()
}

// Helper function to find which keywords were matched and format them as hashtags
func findMatchedKeywords(text string, filter KeywordFilter) []string {
	lowerText := strings.ToLower(text)
	var matched []string

	for _, keyword := range filter.Include {
		if strings.Contains(lowerText, keyword) {
			formatted := strings.ToLower(keyword)
			formatted = strings.ReplaceAll(formatted, " ", "-")
			matched = append(matched, "#"+formatted)
		}
	}

	return matched
}
