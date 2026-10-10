package ua

import "testing"

// The crawler list is only allowed to hold names a browser never sends, so the cases that must NOT match
// matter more than the ones that must: a false positive silently deletes a real visitor from every number
// on the dashboard, and nothing about it looks wrong afterwards.
func TestIsCrawler(t *testing.T) {
	tests := []struct {
		name      string
		userAgent string
		want      bool
	}{
		{"a browser", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120.0.0.0 Safari/537.36", false},
		{"googlebot", "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)", true},
		{"bingbot", "Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)", true},
		{"baiduspider", "Mozilla/5.0 (compatible; Baiduspider/2.0; +http://www.baidu.com/search/spider.html)", true},
		{"telegram", "TelegramBot (like TwitterBot)", true},
		{"slack", "Slackbot-LinkExpanding 1.0 (+https://api.slack.com/robots)", true},
		{"discord", "Mozilla/5.0 (compatible; Discordbot/2.0; +https://discordapp.com)", true},
		{"facebook", "facebookexternalhit/1.1 (+http://www.facebook.com/externalhit_uatext.php)", true},
		{"twitter", "Twitterbot/1.0", true},
		{"curl", "curl/8.4.0", true},
		{"no user agent at all", "", true},

		// The in-app browsers that must survive the list, which is the reason the guide page exists at all.
		//
		// WeChat's preview fetcher sends this same User-Agent, so putting it here would throw away every
		// WeChat visitor together with the prefetch. What separates them is JavaScript: the page confirms
		// itself when it runs, and a fetcher that only reads HTML never gets that far.
		{"wechat in-app browser", "Mozilla/5.0 (iPhone; CPU iPhone OS 16_0 like Mac OS X) AppleWebKit/605.1.15 MicroMessenger/8.0.40", false},
		{"qq in-app browser", "Mozilla/5.0 (Linux; Android 13) AppleWebKit/537.36 MQQBrowser/13.0 Mobile Safari/537.36 QQ/9.0", false},
		// The same trap with a different name: WhatsApp's in-app browser sends WhatsApp/2.x, and so does
		// its preview fetcher. Leaving it out under-counts prefetch and cannot cost a visitor.
		{"whatsapp in-app browser", "Mozilla/5.0 (iPhone; CPU iPhone OS 16_0 like Mac OS X) AppleWebKit/605.1.15 Mobile/15E148 WhatsApp/2.23.20", false},
		// An in-app browser whose name contains a crawler's, which is why the list holds "sogou web spider"
		// and not "sogou".
		{"sogou mobile browser", "Mozilla/5.0 (Linux; Android 13) AppleWebKit/537.36 SogouMobileBrowser/10.0", false},
		// The app, not the fetcher: they are different clients and only one of them is on the list.
		{"telegram app", "Mozilla/5.0 (Linux; Android 13) AppleWebKit/537.36 Telegram/10.2.0", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsCrawler(tt.userAgent); got != tt.want {
				t.Errorf("IsCrawler(%q) = %v, want %v", tt.userAgent, got, tt.want)
			}
		})
	}
}
