# nba_stats_fetcher.py
from nba_api.live.nba.endpoints import scoreboard
from nba_api.stats.endpoints import leaguedashteamstats
import json
import sys

def get_team_stats():
    try:
        team_stats = leaguedashteamstats.LeagueDashTeamStats(
            per_mode_detailed='PerGame',
            season='2023-24',
            season_type_all_star='Regular Season'
        )
        
        stats_dict = team_stats.get_dict()
        processed_stats = {}
        
        for row in stats_dict['resultSets'][0]['rowSet']:
            team_name = f"{row[1]}"  # Full team name
            wins = float(row[3])
            losses = float(row[4])
            
            processed_stats[team_name] = {
                'win_rate': wins / (wins + losses) if (wins + losses) > 0 else 0,
                'avg_points_for': float(row[26]),
                'avg_points_against': float(row[27]),
                'last_ten_games': calculate_last_ten(wins, losses, row[11])
            }
        
        return processed_stats
    except Exception as e:
        print(f"Error in get_team_stats: {e}", file=sys.stderr)
        return {}

def calculate_last_ten(wins, losses, l10):
    if not l10:
        return [False] * 10
    try:
        wins_l10 = int(str(l10).split('-')[0])
        return [True] * wins_l10 + [False] * (10 - wins_l10)
    except:
        return [False] * 10

def get_live_scores():
    try:
        board = scoreboard.ScoreBoard()
        data = board.get_dict()
        
        live_scores = {}
        games = data.get('scoreboard', {}).get('games', [])
        
        for game in games:
            home_team = game.get('homeTeam', {})
            away_team = game.get('awayTeam', {})
            
            game_status = {
                'period': int(game.get('period', 0)),
                'clock': game.get('gameStatusText', '').split(' ')[-1],  # Extract just the time
                'home_score': int(home_team.get('score', 0)),
                'away_score': int(away_team.get('score', 0)),
                'home_team': f"{home_team.get('teamCity', '')} {home_team.get('teamName', '')}",
                'away_team': f"{away_team.get('teamCity', '')} {away_team.get('teamName', '')}",
                'status': int(game.get('gameStatus', 1))
            }
            
            game_key = f"{game_status['away_team']} vs {game_status['home_team']}"
            live_scores[game_key] = game_status
        
        return live_scores
    except Exception as e:
        print(f"Error in get_live_scores: {e}", file=sys.stderr)
        return {}




if __name__ == "__main__":
    try:
        stats = get_team_stats()
        live_scores = get_live_scores()
        
        # Output even if one of them is empty
        output = {
            'stats': stats,
            'live_scores': live_scores
        }
        
        print(json.dumps(output))
    except Exception as e:
        print(json.dumps({"error": str(e)}), file=sys.stderr)
        sys.exit(1)