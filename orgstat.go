package main // import "arp242.net/orgstat"

import (
	"flag"
	"fmt"
	"html/template"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"arp242.net/hubhub"
)

type config struct {
	org   string
	user  string
	token string
	out   string
}

func main() {
	var c config
	flag.StringVar(&c.org, "org", "", "GitHub organisation name")
	flag.StringVar(&c.user, "user", "", "GitHub user")
	flag.StringVar(&c.token, "token", "", "GitHub access token")
	flag.StringVar(&c.out, "out", "", "Output file; - for stdout")

	flag.Parse()

	if c.org == "" {
		flag.Usage()
		stderr("need an organisation name")
		os.Exit(1)
	}
	if c.user == "" {
		flag.Usage()
		stderr("need a username")
		os.Exit(1)
	}
	if c.token == "" {
		flag.Usage()
		stderr("need an access token")
		os.Exit(1)
	}
	if c.out == "" {
		flag.Usage()
		stderr("need an output file")
		os.Exit(1)
	}

	var fp io.Writer
	if c.out == "-" {
		fp = os.Stdout
	} else {
		var err error
		fp, err = os.Create(c.out)
		if err != nil {
			stderr("could not open output: %v", err)
			os.Exit(1)
		}
	}

	hubhub.User = c.user
	hubhub.Token = c.token

	var info struct {
		PublicRepos  int `json:"public_repos"`
		PrivateRepos int `json:"total_private_repos"`
	}

	_, err := hubhub.Request(&info, "GET", "/orgs/"+c.org)
	if err != nil {
		stderr("could not count repos: %v", err)
		os.Exit(1)
	}

	var repos []struct {
		Name     string    `json:"name"`
		Archived bool      `json:"archived"`
		Language string    `json:"language"`
		PushedAt time.Time `json:"pushed_at"`
		Topics   []string  `json:"topics"`
	}

	err = hubhub.Paginate(&repos, "/orgs/"+c.org+"/repos",
		int(math.Ceil((float64(info.PublicRepos)+float64(info.PrivateRepos))/30.0)))
	if err != nil {
		stderr("could not list repos: %v", err)
		os.Exit(1)
	}

	repoNames := make([]string, len(repos))
	for i := range repos {
		repoNames[i] = repos[i].Name
	}

	var (
		totals    = make(map[string]*authorRepoStat)
		lastYear  = make(map[string]*authorRepoStat)
		lastMonth = make(map[string]*authorRepoStat)
		lastWeek  = make(map[string]*authorRepoStat)
		ch        = make(chan bool, len(repos))
		lock      sync.Mutex
	)

	for i := range repos {
		ch <- true
	start:
		if len(ch) >= 8 {
			time.Sleep(100 * time.Millisecond)
			goto start
		}

		go func(i int) {
			defer func() { <-ch }()

			lock.Lock()
			fmt.Printf("  %d/%d %s                         \r", i+1, len(repos), repoNames[i])
			r := repoNames[i]
			lock.Unlock()

			s, err := getStats(c, r)
			if err != nil {
				stderr("\ncould not get stats for %v: %v", r, err)
				//os.Exit(1)
			}

			lock.Lock()
			for _, a := range s.AuthorStats {
				if _, ok := totals[a.Author.Login]; !ok {
					totals[a.Author.Login] = &authorRepoStat{}
					totals[a.Author.Login].Author = a.Author
					lastYear[a.Author.Login] = &authorRepoStat{}
					lastYear[a.Author.Login].Author = a.Author
					lastMonth[a.Author.Login] = &authorRepoStat{}
					lastMonth[a.Author.Login].Author = a.Author
					lastWeek[a.Author.Login] = &authorRepoStat{}
					lastWeek[a.Author.Login].Author = a.Author
				}

				totals[a.Author.Login].Total.Commits += a.Total.Commits
				totals[a.Author.Login].Total.Additions += a.Total.Additions
				totals[a.Author.Login].Total.Deletions += a.Total.Deletions

				lastWeek[a.Author.Login].Total.Commits += a.LastWeek.Commits
				lastWeek[a.Author.Login].Total.Additions += a.LastWeek.Additions
				lastWeek[a.Author.Login].Total.Deletions += a.LastWeek.Deletions

				lastMonth[a.Author.Login].Total.Commits += a.LastMonth.Commits
				lastMonth[a.Author.Login].Total.Additions += a.LastMonth.Additions
				lastMonth[a.Author.Login].Total.Deletions += a.LastMonth.Deletions

				lastYear[a.Author.Login].Total.Commits += a.LastYear.Commits
				lastYear[a.Author.Login].Total.Additions += a.LastYear.Additions
				lastYear[a.Author.Login].Total.Deletions += a.LastYear.Deletions

				// TODO: record which repos.
				totals[a.Author.Login].Total.NumRepos++
				if len(a.Weeks) > 0 {
					t := time.Unix(a.Weeks[len(a.Weeks)-1].WeekStart, 0)

					if t.After(weekAgo) {
						lastWeek[a.Author.Login].Total.NumRepos++
					}
					if t.After(monthAgo) {
						lastMonth[a.Author.Login].Total.NumRepos++
					}
					if t.After(yearAgo) {
						lastYear[a.Author.Login].Total.NumRepos++
					}
				}
			}
			lock.Unlock()
		}(i)
	}
	for len(ch) == 0 {
		<-ch
	}

	fmt.Println()

	stats := make([]repoStat, 4)

	stats[0].Name = "Totals"
	stats[0].AuthorStats = make([]authorRepoStat, len(totals))
	var i int
	for _, t := range totals {
		stats[0].AuthorStats[i] = *t
		i++
	}
	sort.Slice(stats[0].AuthorStats, func(i, j int) bool {
		return stats[0].AuthorStats[i].Total.Commits > stats[0].AuthorStats[j].Total.Commits
	})
	stats[0].AuthorStats = stats[0].AuthorStats[:100]

	stats[1].Name = "Last year"
	stats[1].AuthorStats = make([]authorRepoStat, len(lastYear))
	i = 0
	for _, t := range lastYear {
		stats[1].AuthorStats[i] = *t
		i++
	}
	sort.Slice(stats[1].AuthorStats, func(i, j int) bool {
		return stats[1].AuthorStats[i].Total.Commits > stats[1].AuthorStats[j].Total.Commits
	})
	stats[1].AuthorStats = stats[1].AuthorStats[:100]

	stats[2].Name = "Last month"
	stats[2].AuthorStats = make([]authorRepoStat, len(lastMonth))
	i = 0
	for _, t := range lastMonth {
		stats[2].AuthorStats[i] = *t
		i++
	}
	sort.Slice(stats[2].AuthorStats, func(i, j int) bool {
		return stats[2].AuthorStats[i].Total.Commits > stats[2].AuthorStats[j].Total.Commits
	})
	stats[2].AuthorStats = stats[2].AuthorStats[:100]

	stats[3].Name = "Last week"
	stats[3].AuthorStats = make([]authorRepoStat, len(lastWeek))
	i = 0
	for _, t := range lastWeek {
		stats[3].AuthorStats[i] = *t
		i++
	}
	sort.Slice(stats[3].AuthorStats, func(i, j int) bool {
		return stats[3].AuthorStats[i].Total.Commits > stats[3].AuthorStats[j].Total.Commits
	})
	stats[3].AuthorStats = stats[3].AuthorStats[:100]

	err = tpl.Execute(fp, map[string]interface{}{
		"Stats": stats,
	})
	if err != nil {
		stderr("could not list repos: %v", err)
		os.Exit(1)
	}
}

type repoStat struct {
	Name        string
	AuthorStats []authorRepoStat
}

type authorRepoStat struct {
	Weeks []struct {
		WeekStart int64 `json:"w"`
		Additions int64 `json:"a"`
		Deletions int64 `json:"d"`
		Commits   int64 `json:"c"`
	} `json:"weeks"`

	Author struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		AvatarURL string `json:"avatar_url"`
	} `json:"author"`

	Total struct {
		Additions int64
		Deletions int64
		Commits   int64
		NumRepos  int64
	} `json:"-"`
	LastWeek struct {
		Additions int64
		Deletions int64
		Commits   int64
		NumRepos  int64
	} `json:"-"`
	LastMonth struct {
		Additions int64
		Deletions int64
		Commits   int64
		NumRepos  int64
	} `json:"-"`
	LastYear struct {
		Additions int64
		Deletions int64
		Commits   int64
		NumRepos  int64
	} `json:"-"`
}

var (
	weekAgo  = time.Now().UTC().Add(-168 * time.Hour)
	monthAgo = time.Now().UTC().Add(-720 * time.Hour)
	yearAgo  = time.Now().UTC().Add(-8760 * time.Hour)
)

func getStats(c config, repo string) (*repoStat, error) {
	stats := repoStat{Name: repo}
	_, err := hubhub.Request(&stats.AuthorStats, "GET",
		fmt.Sprintf("/repos/%s/%s/stats/contributors", c.org, repo))
	if err != nil {
		return nil, err
	}

	for i := range stats.AuthorStats {
		for _, w := range stats.AuthorStats[i].Weeks {
			stats.AuthorStats[i].Total.Additions += w.Additions
			stats.AuthorStats[i].Total.Commits += w.Commits
			stats.AuthorStats[i].Total.Deletions += w.Deletions

			t := time.Unix(w.WeekStart, 0)
			if t.After(weekAgo) {
				stats.AuthorStats[i].LastWeek.Additions += w.Additions
				stats.AuthorStats[i].LastWeek.Commits += w.Commits
				stats.AuthorStats[i].LastWeek.Deletions += w.Deletions
			}
			if t.After(monthAgo) {
				stats.AuthorStats[i].LastMonth.Additions += w.Additions
				stats.AuthorStats[i].LastMonth.Commits += w.Commits
				stats.AuthorStats[i].LastMonth.Deletions += w.Deletions
			}
			if t.After(yearAgo) {
				stats.AuthorStats[i].LastYear.Additions += w.Additions
				stats.AuthorStats[i].LastYear.Commits += w.Commits
				stats.AuthorStats[i].LastYear.Deletions += w.Deletions
			}
		}

		// Clear weeks as it's not longer useful.
		//stats.AuthorStats[i].Weeks = nil
	}

	// TODO sort.Sort(stats.AuthorStats)
	return &stats, nil
}

func stderr(msg string, a ...interface{}) {
	_, _ = fmt.Fprintf(os.Stderr, os.Args[0]+": "+msg+"\n", a...)
}

var funcMap = template.FuncMap{
	"add":  func(a, b int) int { return a + b },
	"fmtN": comma,
}

var tpl = template.Must(template.New("tpl").Funcs(funcMap).Parse(
	`<!DOCTYPE html>
<html lang="en">
<head>
	<meta http-equiv="Content-Type" content="text/html; charset=utf-8">
	<meta name="viewport" content="width=device-width, initial-scale=1">
	<title>Github stats</title>

	<style>
		body {
			font-family: sans-serif;
		}
		a {
			text-decoration: none;
			color: rgb(0, 0, 238);
		}
		a:visited {
			color: rgb(0, 0, 238);
		}
		.repo {
		}

		.author {
			border: 1px solid #000;
			border-radius: 2px;
			margin: 5px;
			padding: 5px;
			height: 38px;
			display: inline-block;
			width: 480px;
		}
		.author img {
			border-radius: 2px;
			float: left;
			margin-right: 5px;
		}
		.author h3 {
			font-size: 20px;
			display: inline;
		}
		.rank {
			float: right;
			color: #6a737d;
		}
		.stats { font-size: 14px; }
		.add { color: #28a745; }
		.del { color: #cb2431; }
	</style>
</head>
<body>

	{{range $s := .Stats}}
		<h2>{{$s.Name}}</h2>

		<div class="repo">
			{{range $i, $a := $s.AuthorStats}}
				<div class="author">
					<img src="{{$a.Author.AvatarURL}}" alt="" height="38" width="38">
					<h3><a href="https://github.com/{{$a.Author.Login}}">{{$a.Author.Login}}</a></h3>
					<span class="rank">#{{add $i 1}}</span>

					<div class="stats js-total">
						{{fmtN $a.Total.Commits}} commits in {{fmtN $a.Total.NumRepos}} repos
						<span class="add">{{fmtN $a.Total.Additions}} ++</span>
						<span class="del">{{fmtN $a.Total.Deletions}} --</span>
					</div>
				</div>
			{{end}}
		</div>
	{{end}}

</body>
</html>
`))

// Comma produces a string form of the given number in base 10 with
// commas after every three orders of magnitude.
//
// e.g. Comma(834142) -> 834,142
//
// Copied from https://github.com/dustin/go-humanize/blob/master/comma.go
//
// Copyright (c) 2005-2008  Dustin Sallings <dustin@spy.net>
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.
//
// <http://www.opensource.org/licenses/mit-license.php>
func comma(v int64) string {
	sign := ""

	// Min int64 can't be negated to a usable value, so it has to be special cased.
	if v == math.MinInt64 {
		return "-9,223,372,036,854,775,808"
	}

	if v < 0 {
		sign = "-"
		v = 0 - v
	}

	parts := []string{"", "", "", "", "", "", ""}
	j := len(parts) - 1

	for v > 999 {
		parts[j] = strconv.FormatInt(v%1000, 10)
		switch len(parts[j]) {
		case 2:
			parts[j] = "0" + parts[j]
		case 1:
			parts[j] = "00" + parts[j]
		}
		v = v / 1000
		j--
	}
	parts[j] = strconv.Itoa(int(v))
	return sign + strings.Join(parts[j:], ",")
}
