package main

import (
    "encoding/json"
    "fmt"
    "io/ioutil"
    "math"
    "net/http"
    "os/exec"
    "sort"
    "strings"
    "time"
)

const baseURL = "https://api.the-odds-api.com/v4/sports"

func loadAPIKey() (string, error) {
    data, err := ioutil.ReadFile("api.txt")
    if err != nil {
        return "", fmt.Errorf("error reading api.txt: %v", err)
    }
    return strings.TrimSpace(string(data)), nil
}

func initClient() (string, error) {
    apiKey, err := loadAPIKey()
    if err != nil {
        return "", fmt.Errorf("failed to load API key: %v", err)
    }
    
    if apiKey == "" {
        return "", fmt.Errorf("API key is empty - please check api.txt")
    }
    
    return apiKey, nil
}

type Outcome struct {
	Name  string  `json:"name"`
	Price float64 `json:"price"`
}

type Market struct {
	Key          string    `json:"key"`
	LastUpdate   time.Time `json:"last_update"`
	Outcomes     []Outcome `json:"outcomes"`
	BookmakerKey string    `json:"bookmaker_key"`
}

type Bookmaker struct {
	Key     string   `json:"key"`
	Title   string   `json:"title"`
	Markets []Market `json:"markets"`
}

type Game struct {
	ID         string      `json:"id"`
	SportKey   string      `json:"sport_key"`
	SportTitle string      `json:"sport_title"`
	HomeTeam   string      `json:"home_team"`
	AwayTeam   string      `json:"away_team"`
	Bookmakers []Bookmaker `json:"bookmakers"`
}

type ValueBet struct {
	Game           string
	Team           string
	Odds           float64
	ImpliedProb    float64
	HistoricalProb float64
	Value          float64
	NetRating      float64
	Confidence     float64
}


type TeamStats struct {
	WinRate         float64 `json:"win_rate"`
	AvgPointsFor    float64 `json:"avg_points_for"`
	AvgPointsAgainst float64 `json:"avg_points_against"`
	LastTenGames    []bool  `json:"last_ten_games"` 
}

type NBATeam struct {
	TeamID      int     `json:"TeamID"`
	TeamName    string  `json:"TeamName"`  
	WinPct      float64 `json:"WinPct"`
	Pts         float64 `json:"Pts"`
	PtsAgainst  float64 `json:"PtsAgainst"`
	LastNGames  int     `json:"LastNGames"`
	WinStreak   int     `json:"WinStreak"`
}

type NBAStatsResponse struct {
	League struct {
		Standard []NBATeam `json:"standard"`
	} `json:"league"`
}

type LiveGameState struct {
    Period    int     `json:"period"`
    Clock     string  `json:"clock"`
    HomeScore int     `json:"home_score"`
    AwayScore int     `json:"away_score"`
    HomeTeam  string  `json:"home_team"`
    AwayTeam  string  `json:"away_team"`
    Status    int     `json:"status"`
}

func calculateTimeRemaining(period int, clock string) float64 {
    var minutes float64
    if period <= 4 {
        minutes = float64((4 - period) * 12)
    } else {
        // Overtime
        minutes = 5.0
    }
    
    // Parse clock string (MM:SS)
    var min, sec float64
    fmt.Sscanf(clock, "%f:%f", &min, &sec)
    minutes += min + (sec / 60.0)
    
    return minutes
}

// Helper function to determine if a live bet is viable
func isViableLiveBet(bet ValueBet, liveGame LiveGameState, isHome bool) bool {
    scoreDiff := liveGame.HomeScore - liveGame.AwayScore
    if !isHome {
        scoreDiff = -scoreDiff
    }
    
    timeRemaining := calculateTimeRemaining(liveGame.Period, liveGame.Clock)
    
    // Historical comeback thresholds
    if scoreDiff < -20 && timeRemaining < 15 {
        return false
    } else if scoreDiff < -15 && timeRemaining < 10 {
        return false
    } else if scoreDiff < -10 && timeRemaining < 6 {
        return false
    } else if scoreDiff < -5 && timeRemaining < 2 {
        return false
    }
    
    return true
}

// Helper function to calculate live win probability based on score and time
func calculateLiveWinProbability(scoreDiff int, timeRemaining float64) float64 {
    // Base win probability for team in lead
    if scoreDiff == 0 {
        return 0.5
    }
    
    // Constants based on historical NBA data
    const (
        baseProb = 0.5
        maxProb = 0.99
        minProb = 0.01
    )
    
    // Adjust significance of lead based on time remaining
    // Lead becomes more significant as time decreases
    significance := (48.0 - timeRemaining) / 48.0
    
    // Calculate win probability
    prob := baseProb + (float64(scoreDiff) * 0.03 * significance)
    
    // Ensure probability stays within bounds
    if prob > maxProb {
        return maxProb
    } else if prob < minProb {
        return minProb
    }
    
    return prob
}

func assessComebackProbability(scoreDiff int, timeRemaining float64) bool {
    // Historical NBA comeback data suggests:
    // - 20 point deficit needs ~15 minutes
    // - 15 point deficit needs ~10 minutes
    // - 10 point deficit needs ~6 minutes
    // - 5 point deficit needs ~2 minutes
    
    absScoreDiff := math.Abs(float64(scoreDiff))
    
    if absScoreDiff > 20 && timeRemaining < 15 {
        return false
    } else if absScoreDiff > 15 && timeRemaining < 10 {
        return false
    } else if absScoreDiff > 10 && timeRemaining < 6 {
        return false
    } else if absScoreDiff > 5 && timeRemaining < 2 {
        return false
    }
    
    return true
}

func calculateValue(game Game, stats map[string]TeamStats, liveScores map[string]LiveGameState) []ValueBet {
    var valueBets []ValueBet
    
    // Check if game is live
    gameKey := fmt.Sprintf("%s vs %s", game.AwayTeam, game.HomeTeam)
    liveGame, isLive := liveScores[gameKey]
    
    // Skip finished games
    if isLive && liveGame.Status == 3 {
        return valueBets
    }

    for _, bookmaker := range game.Bookmakers {
        if bookmaker.Key != "betmgm" {
            continue
        }

        for _, market := range bookmaker.Markets {
            if market.Key != "h2h" {
                continue
            }

            for _, outcome := range market.Outcomes {
                if stats, exists := stats[outcome.Name]; exists {
                    impliedProb := americanToImpliedProb(outcome.Price)
                    recentForm := calculateRecentForm(stats.LastTenGames)
                    
                    // Base historical probability
                    historicalProb := (stats.WinRate*0.7 + recentForm*0.3)
                    
                    // Adjust for home court advantage (3-4% historically)
                    if outcome.Name == game.HomeTeam {
                        historicalProb += 0.035
                    } else {
                        historicalProb -= 0.035
                    }

                    // If game is live, adjust probabilities based on score
                    if isLive && liveGame.Status == 2 {
                        scoreDiff := liveGame.HomeScore - liveGame.AwayScore
                        if outcome.Name == game.AwayTeam {
                            scoreDiff = -scoreDiff
                        }
                        
                        timeRemaining := calculateTimeRemaining(liveGame.Period, liveGame.Clock)
                        
                        // Calculate win probability adjustment based on score and time
                        probAdjustment := calculateLiveWinProbability(scoreDiff, timeRemaining)
                        
                        // Blend original probability with live game state
                        historicalProb = (historicalProb * 0.2) + (probAdjustment * 0.8)
                    }
                    
                    // Net rating (points scored vs points allowed)
                    netRating := stats.AvgPointsFor - stats.AvgPointsAgainst
                    
                    // Calculate value (difference between actual probability and implied probability)
                    value := historicalProb - impliedProb
                    
                    // Calculate confidence score
                    confidence := calculateConfidence(value, netRating, recentForm)

                    if value > 0 {
                        valueBet := ValueBet{
                            Game:           gameKey,
                            Team:           outcome.Name,
                            Odds:           outcome.Price,
                            ImpliedProb:    impliedProb,
                            HistoricalProb: historicalProb,
                            Value:          value,
                            NetRating:      netRating,
                            Confidence:     confidence,
                        }
                        
                        // If game is live, check if bet is still viable
                        if isLive && liveGame.Status == 2 {
                            if isViableLiveBet(valueBet, liveGame, outcome.Name == game.HomeTeam) {
                                valueBets = append(valueBets, valueBet)
                            }
                        } else {
                            valueBets = append(valueBets, valueBet)
                        }
                    }
                }
            }
        }
    }

    return valueBets
}

// Add missing helper functions
func americanToImpliedProb(americanOdds float64) float64 {
	if americanOdds > 0 {
		return 100 / (americanOdds + 100)
	}
	return (-americanOdds) / (-americanOdds + 100)
}


type Sport struct {
	Key      string `json:"key"`
	Group    string `json:"group"`
	Title    string `json:"title"`
	Active   bool   `json:"active"`
	HasOdds  bool   `json:"has_odds"`
}

func fetchNBAStats() (map[string]TeamStats, error) {
    cmd := exec.Command("python", "nba_stats_fetcher.py")
    output, err := cmd.Output()
    if err != nil {
        return nil, fmt.Errorf("error running Python script: %v", err)
    }

    var rawStats map[string]struct {
        WinRate         float64 `json:"win_rate"`
        AvgPointsFor    float64 `json:"avg_points_for"`
        AvgPointsAgainst float64 `json:"avg_points_against"`
        LastTenGames    []bool  `json:"last_ten_games"`
    }

    if err := json.Unmarshal(output, &rawStats); err != nil {
        return nil, fmt.Errorf("error parsing Python output: %v", err)
    }

    // Convert to our TeamStats format
    teamStats := make(map[string]TeamStats)
    for team, stats := range rawStats {
        teamStats[team] = TeamStats{
            WinRate:         stats.WinRate,
            AvgPointsFor:    stats.AvgPointsFor,
            AvgPointsAgainst: stats.AvgPointsAgainst,
            LastTenGames:    stats.LastTenGames,
        }
    }

    return teamStats, nil
}

func fetchSports(apiKey string) ([]Sport, error) {
    url := fmt.Sprintf("%s?apiKey=%s", baseURL, apiKey)
    
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var sports []Sport
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(body, &sports)
	return sports, err
}

func fetchOdds(sportKey string, apiKey string) ([]Game, error) {
	url := fmt.Sprintf("%s/%s/odds?apiKey=%s&regions=us&markets=h2h,spreads&oddsFormat=american", 
		baseURL, sportKey, apiKey)
	
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var games []Game
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(body, &games)
	return games, err
}

func displayBetMGMOdds(games []Game) {
	for _, game := range games {
		fmt.Printf("\n%s vs %s\n", game.HomeTeam, game.AwayTeam)
		fmt.Printf("----------------------------------------\n")

		for _, bookmaker := range game.Bookmakers {
			if bookmaker.Key == "betmgm" {
				fmt.Printf("BetMGM Odds:\n")
				for _, market := range bookmaker.Markets {
					fmt.Printf("\nMarket: %s\n", market.Key)
					for _, outcome := range market.Outcomes {
						fmt.Printf("  %s: %+v\n", outcome.Name, outcome.Price)
					}
				}
			}
		}
	}
}

// Update fetchTeamStats to use TeamName instead of Name
func fetchTeamStats() (map[string]TeamStats, error) {
    cmd := exec.Command("python", "nba_stats_fetcher.py")
    output, err := cmd.Output()
    if err != nil {
        return nil, fmt.Errorf("error running Python script: %v", err)
    }

    var rawStats map[string]struct {
        WinRate         float64 `json:"win_rate"`
        AvgPointsFor    float64 `json:"avg_points_for"`
        AvgPointsAgainst float64 `json:"avg_points_against"`
        LastTenGames    []bool  `json:"last_ten_games"`
    }

    if err := json.Unmarshal(output, &rawStats); err != nil {
        return nil, fmt.Errorf("error parsing Python output: %v", err)
    }

    // Convert to our TeamStats format
    teamStats := make(map[string]TeamStats)
    for team, stats := range rawStats {
        teamStats[team] = TeamStats{
            WinRate:         stats.WinRate,
            AvgPointsFor:    stats.AvgPointsFor,
            AvgPointsAgainst: stats.AvgPointsAgainst,
            LastTenGames:    stats.LastTenGames,
        }
    }

    return teamStats, nil
}

// Helper function to estimate last 10 games from win streak
func calculateLastTenFromStreak(streak, lastN int) []bool {
	games := make([]bool, 10)
	if streak > 0 {
		// Team is on a winning streak
		for i := 0; i < min(streak, 10); i++ {
			games[i] = true
		}
	} else {
		// Team is on a losing streak
		for i := 0; i < min(-streak, 10); i++ {
			games[i] = false
		}
	}
	return games
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func calculateRecentForm(games []bool) float64 {
	if len(games) == 0 {
		return 0.5 // Default to 50% if no games data
	}
	
	wins := 0
	for _, win := range games {
		if win {
			wins++
		}
	}
	return float64(wins) / float64(len(games))
}

func calculateConfidence(value, netRating, recentForm float64) float64 {
    valueWeight := 0.5
    netRatingWeight := 0.3
    recentFormWeight := 0.2

    // Normalize value (typical range -0.3 to 0.3)
    normalizedValue := math.Min(math.Max(value*3, 0), 1.0)
    
    // Normalize net rating (typical range -10 to +10)
    normalizedNetRating := (netRating + 10) / 20
    
    // Combine factors with exponential scaling to make high confidence harder
    confidence := (normalizedValue * valueWeight) +
        (normalizedNetRating * netRatingWeight) +
        (recentForm * recentFormWeight)
    
    // Apply exponential scaling to make high confidence harder to achieve
    return math.Pow(math.Max(0, math.Min(1, confidence)), 1.5)
}

func main() {
    fmt.Println("Starting NBA betting analysis...")
    
    // Load API key first
    apiKey, err := loadAPIKey()
    if err != nil {
        fmt.Printf("Error loading API key: %v\n", err)
        return
    }

    // Step 1: Fetch NBA stats and live scores from Python script
    fmt.Println("\nFetching NBA data and live scores...")
    var combinedData struct {
        Stats      map[string]TeamStats     `json:"stats"`
        LiveScores map[string]LiveGameState `json:"live_scores"`
    }

    cmd := exec.Command("python", "nba_stats_fetcher.py")
    output, err := cmd.CombinedOutput()
    if err != nil {
        fmt.Printf("Error running Python script: %v\n", err)
        fmt.Printf("Python output: %s\n", string(output))
        return
    }

    if err := json.Unmarshal(output, &combinedData); err != nil {
        fmt.Printf("Error parsing Python output: %v\n", err)
        fmt.Printf("Raw output: %s\n", string(output))
        return
    }

    fmt.Printf("Successfully loaded data for %d teams and %d live games\n", 
        len(combinedData.Stats), len(combinedData.LiveScores))

    // Step 2: Fetch available sports - passing apiKey
    fmt.Println("\nFetching available sports...")
    sports, err := fetchSports(apiKey)  // Fixed: passing apiKey
    if err != nil {
        fmt.Printf("Error fetching sports: %v\n", err)
        return
    }

    fmt.Println("Available Sports:")
    for _, sport := range sports {
        fmt.Printf("- %s (%s)\n", sport.Title, sport.Key)
    }

    // Step 3: Fetch NBA odds - passing apiKey
    fmt.Println("\nFetching NBA odds...")
    games, err := fetchOdds("basketball_nba", apiKey)  // Fixed: passing apiKey
    if err != nil {
        fmt.Printf("Error fetching odds: %v\n", err)
        return
    }
    fmt.Printf("Successfully fetched odds for %d games\n", len(games))

    // Step 4: Display current odds
    fmt.Println("\nCurrent BetMGM Odds:")
    displayBetMGMOdds(games)

    // Step 5: Analyze betting opportunities
    fmt.Println("\nAnalyzing value betting opportunities...")
    analyzeValueBets(games, combinedData.Stats, combinedData.LiveScores)

    // Optional: Display any live game information
    if len(combinedData.LiveScores) > 0 {
        fmt.Println("\nCurrent Live Games:")
        for gameKey, liveGame := range combinedData.LiveScores {
            fmt.Printf("\n%s:\n", gameKey)
            fmt.Printf("Period: %d, Clock: %s\n", liveGame.Period, liveGame.Clock)
            fmt.Printf("Score: %s %d - %d %s\n",
                liveGame.HomeTeam, liveGame.HomeScore,
                liveGame.AwayScore, liveGame.AwayTeam)
        }
    }

    fmt.Println("\nAnalysis complete!")
}

func analyzeValueBets(games []Game, teamStats map[string]TeamStats, liveScores map[string]LiveGameState) {
    fmt.Printf("\nValue Betting Analysis:\n")
    fmt.Printf("=============================\n")

    var valueBets []ValueBet
    for _, game := range games {
        bets := calculateValue(game, teamStats, liveScores)
        valueBets = append(valueBets, bets...)
    }

    // Sort by confidence (highest first)
    sort.Slice(valueBets, func(i, j int) bool {
        return valueBets[i].Confidence > valueBets[j].Confidence
    })

    // Display top value bets
    for i, bet := range valueBets {
        if i >= 5 {
            break
        }
        displayValueBet(i+1, bet, liveScores)
    }
}

func displayValueBet(index int, bet ValueBet, liveScores map[string]LiveGameState) {
    fmt.Printf("\nValue Bet #%d:\n", index)
    fmt.Printf("Game: %s\n", bet.Game)
    
    if liveGame, isLive := liveScores[bet.Game]; isLive {
        fmt.Printf("\nLIVE GAME STATUS:\n")
        fmt.Printf("Quarter: %d  Time Remaining: %s\n", liveGame.Period, liveGame.Clock)
        fmt.Printf("Score: %s %d - %d %s\n", 
            liveGame.HomeTeam, liveGame.HomeScore,
            liveGame.AwayScore, liveGame.AwayTeam)
        
        timeRemaining := calculateTimeRemaining(liveGame.Period, liveGame.Clock)
        scoreDiff := liveGame.HomeScore - liveGame.AwayScore
        if bet.Team == liveGame.AwayTeam {
            scoreDiff = -scoreDiff
        }
        
        fmt.Printf("Team Status: %s %+d with %.1f minutes remaining\n",
            bet.Team, scoreDiff, timeRemaining)
    }

    fmt.Printf("\nBETTING ANALYSIS:\n")
    fmt.Printf("Recommended Bet: %s\n", bet.Team)
    fmt.Printf("Current Odds: %+.2f\n", bet.Odds)
    fmt.Printf("Implied Win Probability: %.1f%%\n", bet.ImpliedProb*100)
    fmt.Printf("Historical Win Rate: %.1f%%\n", bet.HistoricalProb*100)
    fmt.Printf("Value Edge: %.1f%%\n", bet.Value*100)
    fmt.Printf("Net Rating: %+.1f\n", bet.NetRating)
    fmt.Printf("Confidence Score: %.3f\n", bet.Confidence)
    
    fmt.Printf("\nRECOMMENDATION:\n")
    if bet.Confidence > 0.6 {
        fmt.Printf("Strong Value Bet - High confidence in favorable odds\n")
    } else if bet.Confidence > 0.3 {
        fmt.Printf("Moderate Value Bet - Decent odds but moderate risk\n")
    } else {
        fmt.Printf("Speculative Bet - Favorable odds but high risk\n")
    }
    
    if bet.Value > 0.15 {
        fmt.Printf("Large value gap detected (>15%%) - Worth strong consideration\n")
    }
    
    fmt.Printf("-------------------\n")
}