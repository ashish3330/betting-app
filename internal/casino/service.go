package casino

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/lotus-exchange/lotus-exchange/internal/wallet"
	"github.com/redis/go-redis/v9"
)

// withSerializableRetry executes fn inside a serializable transaction,
// retrying up to maxRetries times on PostgreSQL serialization failures
// (SQLSTATE 40001).
func withSerializableRetry(ctx context.Context, db *sql.DB, maxRetries int, fn func(tx *sql.Tx) error) error {
	for attempt := 0; attempt <= maxRetries; attempt++ {
		tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
		if err != nil {
			return fmt.Errorf("begin tx: %w", err)
		}

		err = fn(tx)
		if err != nil {
			tx.Rollback()
			if isSerializationFailure(err) && attempt < maxRetries {
				continue
			}
			return err
		}

		err = tx.Commit()
		if err != nil {
			if isSerializationFailure(err) && attempt < maxRetries {
				continue
			}
			return fmt.Errorf("commit: %w", err)
		}
		return nil
	}
	return fmt.Errorf("serializable transaction failed after %d retries", maxRetries)
}

// isSerializationFailure checks whether the error is a PostgreSQL
// serialization failure (SQLSTATE 40001).
func isSerializationFailure(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "40001") || strings.Contains(msg, "could not serialize access")
}

// ---------------------------------------------------------------------------
// Game types -- covers every game on Lotus / myplazone9 platforms
// ---------------------------------------------------------------------------

type GameType string

const (
	// Live Casino -- Indian card games
	GameTeenPatti       GameType = "teen_patti"
	GameAndarBahar      GameType = "andar_bahar"
	GameDragonTiger     GameType = "dragon_tiger"
	GameTeenPatti2020   GameType = "teen_patti_20_20"
	GameOneDayTeenPatti GameType = "one_day_teen_patti"
	GameInstantWorli    GameType = "instant_worli"
	GameThirtyTwoCards  GameType = "thirty_two_cards"

	// Live Casino -- table games
	GameRoulette           GameType = "roulette"
	GameBaccarat           GameType = "baccarat"
	GamePoker              GameType = "poker"
	GameBlackjack          GameType = "blackjack"
	GameLucky7             GameType = "lucky_7"
	GameHiLo               GameType = "hi_lo"
	GameBollywoodCasino    GameType = "bollywood_casino"
	GameCasinoWar          GameType = "casino_war"
	GameSicBo              GameType = "sic_bo"
	GameFanTan             GameType = "fan_tan"
	GameLiveCasinoHoldem   GameType = "live_casino_holdem"
	GameThreeCardPoker     GameType = "three_card_poker"
	GameCaribbeanStud      GameType = "caribbean_stud"
	GameDreamCatcher       GameType = "dream_catcher"
	GameMonopolyLive       GameType = "monopoly_live"
	GameCrazyTime          GameType = "crazy_time"
	GameLightningRoulette  GameType = "lightning_roulette"
	GameLightningBaccarat  GameType = "lightning_baccarat"
	GameSpeedBaccarat      GameType = "speed_baccarat"

	// Virtual Sports
	GameVirtualCricket     GameType = "virtual_cricket"
	GameVirtualFootball    GameType = "virtual_football"
	GameVirtualTennis      GameType = "virtual_tennis"
	GameVirtualHorseRacing GameType = "virtual_horse_racing"
	GameVirtualGreyhound   GameType = "virtual_greyhound"
	GameVirtualSpeedway    GameType = "virtual_speedway"

	// Slots
	GameSlotsClassic     GameType = "slots_classic"
	GameSlotsVideo       GameType = "slots_video"
	GameSlotsProgressive GameType = "slots_progressive"
	GameSlotsMegaways    GameType = "slots_megaways"

	// Crash Games
	GameAviator  GameType = "aviator"
	GameSpaceman GameType = "spaceman"
	GameMines    GameType = "mines"
	GamePlinko   GameType = "plinko"
	GameLimbo    GameType = "limbo"

	// Card / Number Games
	GameWorliMatka  GameType = "worli_matka"
	GameLottery     GameType = "lottery"
	GameNumberGame  GameType = "number_game"
)

// ---------------------------------------------------------------------------
// Game categories
// ---------------------------------------------------------------------------

type GameCategory string

const (
	CategoryLiveCasino    GameCategory = "live_casino"
	CategoryVirtualSports GameCategory = "virtual_sports"
	CategorySlots         GameCategory = "slots"
	CategoryCrashGames    GameCategory = "crash_games"
	CategoryCardGames     GameCategory = "card_games"
	CategoryTableGames    GameCategory = "table_games"
)

// ---------------------------------------------------------------------------
// Game definition
// ---------------------------------------------------------------------------

type Game struct {
	Type      GameType     `json:"type"`
	Name      string       `json:"name"`
	Category  GameCategory `json:"category"`
	Providers []string     `json:"providers"`
	MinBet    float64      `json:"min_bet"`
	MaxBet    float64      `json:"max_bet"`
	Active    bool         `json:"active"`
	Thumbnail string       `json:"thumbnail"`
	SortOrder int          `json:"sort_order"`
}

// ---------------------------------------------------------------------------
// Session / Bet types (unchanged)
// ---------------------------------------------------------------------------

type SessionStatus string

const (
	SessionActive  SessionStatus = "active"
	SessionExpired SessionStatus = "expired"
	SessionClosed  SessionStatus = "closed"
)

type CasinoSession struct {
	ID         string        `json:"id" db:"id"`
	UserID     int64         `json:"user_id" db:"user_id"`
	GameType   GameType      `json:"game_type" db:"game_type"`
	ProviderID string        `json:"provider_id" db:"provider_id"`
	Status     SessionStatus `json:"status" db:"status"`
	StreamURL  string        `json:"stream_url" db:"stream_url"`
	Token      string        `json:"token" db:"token"`
	Balance    float64       `json:"balance" db:"balance"`
	CreatedAt  time.Time     `json:"created_at" db:"created_at"`
	ExpiresAt  time.Time     `json:"expires_at" db:"expires_at"`
}

type CasinoBet struct {
	ID        string    `json:"id" db:"id"`
	SessionID string    `json:"session_id" db:"session_id"`
	UserID    int64     `json:"user_id" db:"user_id"`
	GameType  GameType  `json:"game_type" db:"game_type"`
	RoundID   string    `json:"round_id" db:"round_id"`
	Stake     float64   `json:"stake" db:"stake"`
	Payout    float64   `json:"payout" db:"payout"`
	Status    string    `json:"status" db:"status"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// ---------------------------------------------------------------------------
// Provider
// ---------------------------------------------------------------------------

type Provider struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Games      []GameType `json:"games"`
	StreamBase string     `json:"stream_base"`
	Active     bool       `json:"active"`
}

// ---------------------------------------------------------------------------
// Service
// ---------------------------------------------------------------------------

type Service struct {
	db        *sql.DB
	redis     *redis.Client
	wallet    *wallet.Service
	logger    *slog.Logger
	providers map[string]*Provider
	games     map[GameType]*Game
}

func NewService(db *sql.DB, rdb *redis.Client, walletSvc *wallet.Service, logger *slog.Logger) *Service {
	s := &Service{
		db:     db,
		redis:  rdb,
		wallet: walletSvc,
		logger: logger,
	}
	s.initProviders()
	s.initGames()
	return s
}

// ---------------------------------------------------------------------------
// Provider registry
// ---------------------------------------------------------------------------

func (s *Service) initProviders() {
	s.providers = map[string]*Provider{
		"evolution": {
			ID:   "evolution",
			Name: "Evolution Gaming",
			Games: []GameType{
				GameRoulette, GameBaccarat, GameBlackjack, GamePoker,
				GameLightningRoulette, GameLightningBaccarat, GameSpeedBaccarat,
				GameDreamCatcher, GameMonopolyLive, GameCrazyTime,
				GameLiveCasinoHoldem, GameCaribbeanStud, GameSicBo, GameFanTan,
				GameDragonTiger, GameCasinoWar, GameHiLo,
			},
			StreamBase: "https://stream.evolution.com/hls",
			Active:     true,
		},
		"ezugi": {
			ID:   "ezugi",
			Name: "Ezugi",
			Games: []GameType{
				GameTeenPatti, GameAndarBahar, GameDragonTiger,
				GameRoulette, GameBaccarat, GameBlackjack,
				GameTeenPatti2020, GameOneDayTeenPatti, GameThirtyTwoCards,
				GameLucky7, GameBollywoodCasino,
			},
			StreamBase: "https://stream.ezugi.com/hls",
			Active:     true,
		},
		"betgames": {
			ID:   "betgames",
			Name: "BetGames",
			Games: []GameType{
				GameLucky7, GameHiLo, GamePoker, GameBaccarat, GameWorliMatka,
				GameLottery, GameNumberGame, GameInstantWorli,
			},
			StreamBase: "https://stream.betgames.tv/hls",
			Active:     true,
		},
		"superspade": {
			ID:   "superspade",
			Name: "Super Spade Games",
			Games: []GameType{
				GameTeenPatti, GameAndarBahar, GameDragonTiger,
				GameTeenPatti2020, GameOneDayTeenPatti, GameThirtyTwoCards,
				GamePoker, GameThreeCardPoker, GameBollywoodCasino,
			},
			StreamBase: "https://stream.superspade.com/hls",
			Active:     true,
		},
		"aesexy": {
			ID:   "aesexy",
			Name: "AE Sexy",
			Games: []GameType{
				GameBaccarat, GameDragonTiger, GameRoulette,
				GameSicBo, GameFanTan,
			},
			StreamBase: "https://stream.aesexy.com/hls",
			Active:     true,
		},
		"pragmatic_play": {
			ID:   "pragmatic_play",
			Name: "Pragmatic Play",
			Games: []GameType{
				GameSlotsClassic, GameSlotsVideo, GameSlotsProgressive, GameSlotsMegaways,
				GameAviator, GameSpaceman, GameMines, GamePlinko, GameLimbo,
				GameVirtualCricket, GameVirtualFootball, GameVirtualTennis,
				GameVirtualHorseRacing, GameVirtualGreyhound, GameVirtualSpeedway,
				GameRoulette, GameBaccarat, GameBlackjack,
			},
			StreamBase: "https://stream.pragmaticplay.net/hls",
			Active:     true,
		},
	}
}

// ---------------------------------------------------------------------------
// Full game catalog
// ---------------------------------------------------------------------------

func (s *Service) initGames() {
	s.games = map[GameType]*Game{
		// Live Casino -- Indian card games
		GameTeenPatti:       {Type: GameTeenPatti, Name: "Teen Patti", Category: CategoryLiveCasino, Providers: []string{"ezugi", "superspade"}, MinBet: 100, MaxBet: 500000, Active: true, Thumbnail: "/img/casino/teen_patti.webp", SortOrder: 1},
		GameAndarBahar:      {Type: GameAndarBahar, Name: "Andar Bahar", Category: CategoryLiveCasino, Providers: []string{"ezugi", "superspade"}, MinBet: 100, MaxBet: 500000, Active: true, Thumbnail: "/img/casino/andar_bahar.webp", SortOrder: 2},
		GameDragonTiger:     {Type: GameDragonTiger, Name: "Dragon Tiger", Category: CategoryLiveCasino, Providers: []string{"evolution", "ezugi", "superspade", "aesexy"}, MinBet: 100, MaxBet: 500000, Active: true, Thumbnail: "/img/casino/dragon_tiger.webp", SortOrder: 3},
		GameTeenPatti2020:   {Type: GameTeenPatti2020, Name: "Teen Patti 20-20", Category: CategoryLiveCasino, Providers: []string{"ezugi", "superspade"}, MinBet: 100, MaxBet: 300000, Active: true, Thumbnail: "/img/casino/teen_patti_2020.webp", SortOrder: 4},
		GameOneDayTeenPatti: {Type: GameOneDayTeenPatti, Name: "One Day Teen Patti", Category: CategoryLiveCasino, Providers: []string{"ezugi", "superspade"}, MinBet: 100, MaxBet: 300000, Active: true, Thumbnail: "/img/casino/one_day_tp.webp", SortOrder: 5},
		GameInstantWorli:    {Type: GameInstantWorli, Name: "Instant Worli", Category: CategoryLiveCasino, Providers: []string{"betgames"}, MinBet: 50, MaxBet: 200000, Active: true, Thumbnail: "/img/casino/instant_worli.webp", SortOrder: 6},
		GameThirtyTwoCards:  {Type: GameThirtyTwoCards, Name: "32 Cards", Category: CategoryLiveCasino, Providers: []string{"ezugi", "superspade"}, MinBet: 100, MaxBet: 300000, Active: true, Thumbnail: "/img/casino/32cards.webp", SortOrder: 7},

		// Live Casino -- table games
		GameRoulette:          {Type: GameRoulette, Name: "Roulette", Category: CategoryTableGames, Providers: []string{"evolution", "ezugi", "aesexy", "pragmatic_play"}, MinBet: 100, MaxBet: 1000000, Active: true, Thumbnail: "/img/casino/roulette.webp", SortOrder: 10},
		GameBaccarat:          {Type: GameBaccarat, Name: "Baccarat", Category: CategoryTableGames, Providers: []string{"evolution", "ezugi", "aesexy", "pragmatic_play"}, MinBet: 100, MaxBet: 1000000, Active: true, Thumbnail: "/img/casino/baccarat.webp", SortOrder: 11},
		GamePoker:             {Type: GamePoker, Name: "Poker", Category: CategoryTableGames, Providers: []string{"evolution", "betgames", "superspade"}, MinBet: 100, MaxBet: 500000, Active: true, Thumbnail: "/img/casino/poker.webp", SortOrder: 12},
		GameBlackjack:         {Type: GameBlackjack, Name: "Blackjack", Category: CategoryTableGames, Providers: []string{"evolution", "ezugi", "pragmatic_play"}, MinBet: 500, MaxBet: 1000000, Active: true, Thumbnail: "/img/casino/blackjack.webp", SortOrder: 13},
		GameLucky7:            {Type: GameLucky7, Name: "Lucky 7", Category: CategoryLiveCasino, Providers: []string{"ezugi", "betgames"}, MinBet: 50, MaxBet: 200000, Active: true, Thumbnail: "/img/casino/lucky7.webp", SortOrder: 14},
		GameHiLo:              {Type: GameHiLo, Name: "Hi Lo", Category: CategoryLiveCasino, Providers: []string{"evolution", "betgames"}, MinBet: 50, MaxBet: 200000, Active: true, Thumbnail: "/img/casino/hilo.webp", SortOrder: 15},
		GameBollywoodCasino:   {Type: GameBollywoodCasino, Name: "Bollywood Casino", Category: CategoryLiveCasino, Providers: []string{"ezugi", "superspade"}, MinBet: 100, MaxBet: 300000, Active: true, Thumbnail: "/img/casino/bollywood.webp", SortOrder: 16},
		GameCasinoWar:         {Type: GameCasinoWar, Name: "Casino War", Category: CategoryTableGames, Providers: []string{"evolution"}, MinBet: 100, MaxBet: 500000, Active: true, Thumbnail: "/img/casino/casino_war.webp", SortOrder: 17},
		GameSicBo:             {Type: GameSicBo, Name: "Sic Bo", Category: CategoryTableGames, Providers: []string{"evolution", "aesexy"}, MinBet: 100, MaxBet: 500000, Active: true, Thumbnail: "/img/casino/sicbo.webp", SortOrder: 18},
		GameFanTan:            {Type: GameFanTan, Name: "Fan Tan", Category: CategoryTableGames, Providers: []string{"evolution", "aesexy"}, MinBet: 100, MaxBet: 500000, Active: true, Thumbnail: "/img/casino/fantan.webp", SortOrder: 19},
		GameLiveCasinoHoldem:  {Type: GameLiveCasinoHoldem, Name: "Live Casino Hold'em", Category: CategoryTableGames, Providers: []string{"evolution"}, MinBet: 500, MaxBet: 500000, Active: true, Thumbnail: "/img/casino/holdem.webp", SortOrder: 20},
		GameThreeCardPoker:    {Type: GameThreeCardPoker, Name: "Three Card Poker", Category: CategoryTableGames, Providers: []string{"superspade"}, MinBet: 100, MaxBet: 300000, Active: true, Thumbnail: "/img/casino/3card_poker.webp", SortOrder: 21},
		GameCaribbeanStud:     {Type: GameCaribbeanStud, Name: "Caribbean Stud Poker", Category: CategoryTableGames, Providers: []string{"evolution"}, MinBet: 500, MaxBet: 500000, Active: true, Thumbnail: "/img/casino/caribbean.webp", SortOrder: 22},
		GameDreamCatcher:      {Type: GameDreamCatcher, Name: "Dream Catcher", Category: CategoryLiveCasino, Providers: []string{"evolution"}, MinBet: 100, MaxBet: 500000, Active: true, Thumbnail: "/img/casino/dream_catcher.webp", SortOrder: 23},
		GameMonopolyLive:      {Type: GameMonopolyLive, Name: "Monopoly Live", Category: CategoryLiveCasino, Providers: []string{"evolution"}, MinBet: 100, MaxBet: 500000, Active: true, Thumbnail: "/img/casino/monopoly.webp", SortOrder: 24},
		GameCrazyTime:         {Type: GameCrazyTime, Name: "Crazy Time", Category: CategoryLiveCasino, Providers: []string{"evolution"}, MinBet: 100, MaxBet: 500000, Active: true, Thumbnail: "/img/casino/crazy_time.webp", SortOrder: 25},
		GameLightningRoulette: {Type: GameLightningRoulette, Name: "Lightning Roulette", Category: CategoryTableGames, Providers: []string{"evolution"}, MinBet: 200, MaxBet: 1000000, Active: true, Thumbnail: "/img/casino/lightning_roulette.webp", SortOrder: 26},
		GameLightningBaccarat: {Type: GameLightningBaccarat, Name: "Lightning Baccarat", Category: CategoryTableGames, Providers: []string{"evolution"}, MinBet: 200, MaxBet: 1000000, Active: true, Thumbnail: "/img/casino/lightning_baccarat.webp", SortOrder: 27},
		GameSpeedBaccarat:     {Type: GameSpeedBaccarat, Name: "Speed Baccarat", Category: CategoryTableGames, Providers: []string{"evolution"}, MinBet: 100, MaxBet: 1000000, Active: true, Thumbnail: "/img/casino/speed_baccarat.webp", SortOrder: 28},

		// Virtual Sports
		GameVirtualCricket:     {Type: GameVirtualCricket, Name: "Virtual Cricket", Category: CategoryVirtualSports, Providers: []string{"pragmatic_play"}, MinBet: 50, MaxBet: 100000, Active: true, Thumbnail: "/img/casino/virtual_cricket.webp", SortOrder: 40},
		GameVirtualFootball:    {Type: GameVirtualFootball, Name: "Virtual Football", Category: CategoryVirtualSports, Providers: []string{"pragmatic_play"}, MinBet: 50, MaxBet: 100000, Active: true, Thumbnail: "/img/casino/virtual_football.webp", SortOrder: 41},
		GameVirtualTennis:      {Type: GameVirtualTennis, Name: "Virtual Tennis", Category: CategoryVirtualSports, Providers: []string{"pragmatic_play"}, MinBet: 50, MaxBet: 100000, Active: true, Thumbnail: "/img/casino/virtual_tennis.webp", SortOrder: 42},
		GameVirtualHorseRacing: {Type: GameVirtualHorseRacing, Name: "Virtual Horse Racing", Category: CategoryVirtualSports, Providers: []string{"pragmatic_play"}, MinBet: 50, MaxBet: 100000, Active: true, Thumbnail: "/img/casino/virtual_horse.webp", SortOrder: 43},
		GameVirtualGreyhound:   {Type: GameVirtualGreyhound, Name: "Virtual Greyhound Racing", Category: CategoryVirtualSports, Providers: []string{"pragmatic_play"}, MinBet: 50, MaxBet: 100000, Active: true, Thumbnail: "/img/casino/virtual_greyhound.webp", SortOrder: 44},
		GameVirtualSpeedway:    {Type: GameVirtualSpeedway, Name: "Virtual Speedway", Category: CategoryVirtualSports, Providers: []string{"pragmatic_play"}, MinBet: 50, MaxBet: 100000, Active: true, Thumbnail: "/img/casino/virtual_speedway.webp", SortOrder: 45},

		// Slots
		GameSlotsClassic:     {Type: GameSlotsClassic, Name: "Classic Slots", Category: CategorySlots, Providers: []string{"pragmatic_play"}, MinBet: 10, MaxBet: 50000, Active: true, Thumbnail: "/img/casino/slots_classic.webp", SortOrder: 50},
		GameSlotsVideo:       {Type: GameSlotsVideo, Name: "Video Slots", Category: CategorySlots, Providers: []string{"pragmatic_play"}, MinBet: 10, MaxBet: 50000, Active: true, Thumbnail: "/img/casino/slots_video.webp", SortOrder: 51},
		GameSlotsProgressive: {Type: GameSlotsProgressive, Name: "Progressive Jackpot Slots", Category: CategorySlots, Providers: []string{"pragmatic_play"}, MinBet: 20, MaxBet: 100000, Active: true, Thumbnail: "/img/casino/slots_progressive.webp", SortOrder: 52},
		GameSlotsMegaways:    {Type: GameSlotsMegaways, Name: "Megaways Slots", Category: CategorySlots, Providers: []string{"pragmatic_play"}, MinBet: 20, MaxBet: 100000, Active: true, Thumbnail: "/img/casino/slots_megaways.webp", SortOrder: 53},

		// Crash Games
		GameAviator:  {Type: GameAviator, Name: "Aviator", Category: CategoryCrashGames, Providers: []string{"pragmatic_play"}, MinBet: 10, MaxBet: 100000, Active: true, Thumbnail: "/img/casino/aviator.webp", SortOrder: 60},
		GameSpaceman: {Type: GameSpaceman, Name: "Spaceman", Category: CategoryCrashGames, Providers: []string{"pragmatic_play"}, MinBet: 10, MaxBet: 100000, Active: true, Thumbnail: "/img/casino/spaceman.webp", SortOrder: 61},
		GameMines:    {Type: GameMines, Name: "Mines", Category: CategoryCrashGames, Providers: []string{"pragmatic_play"}, MinBet: 10, MaxBet: 50000, Active: true, Thumbnail: "/img/casino/mines.webp", SortOrder: 62},
		GamePlinko:   {Type: GamePlinko, Name: "Plinko", Category: CategoryCrashGames, Providers: []string{"pragmatic_play"}, MinBet: 10, MaxBet: 50000, Active: true, Thumbnail: "/img/casino/plinko.webp", SortOrder: 63},
		GameLimbo:    {Type: GameLimbo, Name: "Limbo", Category: CategoryCrashGames, Providers: []string{"pragmatic_play"}, MinBet: 10, MaxBet: 50000, Active: true, Thumbnail: "/img/casino/limbo.webp", SortOrder: 64},

		// Card / Number Games
		GameWorliMatka: {Type: GameWorliMatka, Name: "Worli Matka", Category: CategoryCardGames, Providers: []string{"betgames"}, MinBet: 50, MaxBet: 200000, Active: true, Thumbnail: "/img/casino/worli_matka.webp", SortOrder: 70},
		GameLottery:    {Type: GameLottery, Name: "Lottery", Category: CategoryCardGames, Providers: []string{"betgames"}, MinBet: 50, MaxBet: 100000, Active: true, Thumbnail: "/img/casino/lottery.webp", SortOrder: 71},
		GameNumberGame: {Type: GameNumberGame, Name: "Number Game", Category: CategoryCardGames, Providers: []string{"betgames"}, MinBet: 50, MaxBet: 100000, Active: true, Thumbnail: "/img/casino/number_game.webp", SortOrder: 72},
	}
}

// ---------------------------------------------------------------------------
// Query helpers
// ---------------------------------------------------------------------------

func (s *Service) ListProviders() []*Provider {
	var providers []*Provider
	for _, p := range s.providers {
		if p.Active {
			providers = append(providers, p)
		}
	}
	return providers
}

func (s *Service) ListGames() []*Game {
	var games []*Game
	for _, g := range s.games {
		if g.Active {
			games = append(games, g)
		}
	}
	return games
}

// ListGamesByCategory returns all active games in the given category.
func (s *Service) ListGamesByCategory(category GameCategory) []*Game {
	var result []*Game
	for _, g := range s.games {
		if g.Active && g.Category == category {
			result = append(result, g)
		}
	}
	return result
}

// GetGame returns the game definition for a given type, or nil if not found.
func (s *Service) GetGame(gameType GameType) *Game {
	return s.games[gameType]
}

// ListCategories returns all available game categories.
func (s *Service) ListCategories() []GameCategory {
	return []GameCategory{
		CategoryLiveCasino,
		CategoryTableGames,
		CategoryVirtualSports,
		CategorySlots,
		CategoryCrashGames,
		CategoryCardGames,
	}
}

// ---------------------------------------------------------------------------
// Session management (unchanged business logic)
// ---------------------------------------------------------------------------

func (s *Service) CreateSession(ctx context.Context, userID int64, gameType GameType, providerID string) (*CasinoSession, error) {
	provider, ok := s.providers[providerID]
	if !ok || !provider.Active {
		return nil, fmt.Errorf("provider %s not available", providerID)
	}

	hasGame := false
	for _, g := range provider.Games {
		if g == gameType {
			hasGame = true
			break
		}
	}
	if !hasGame {
		return nil, fmt.Errorf("game %s not available on provider %s", gameType, providerID)
	}

	// Validate game exists and check bet limits
	game := s.GetGame(gameType)
	if game == nil || !game.Active {
		return nil, fmt.Errorf("game %s is not available", gameType)
	}

	// REGULATORY: every entry point that lets a user wager money must
	// re-check responsible-gambling state. Casino was previously a hole
	// because launch did not consult responsible_gambling at all.
	if err := s.checkResponsibleGambling(ctx, userID); err != nil {
		return nil, err
	}

	// REGULATORY: KYC must be verified before launching any real-money
	// game. Fails closed on DB error so an outage can't open a hole.
	if err := s.checkKYCVerified(ctx, userID); err != nil {
		return nil, err
	}

	// Enforce concurrent session limit (max 3 active sessions per user).
	var activeCount int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM casino_sessions WHERE user_id = $1 AND status = 'active'`,
		userID,
	).Scan(&activeCount)
	if err != nil {
		return nil, fmt.Errorf("check active sessions: %w", err)
	}
	if activeCount >= 3 {
		return nil, fmt.Errorf("maximum concurrent sessions reached (limit: 3)")
	}

	sessionID := generateSessionID()
	token := generateToken()
	expiresAt := time.Now().Add(4 * time.Hour)

	streamURL := fmt.Sprintf("%s/%s/%s/stream.m3u8?token=%s",
		provider.StreamBase, gameType, sessionID, token)

	session := &CasinoSession{
		ID:         sessionID,
		UserID:     userID,
		GameType:   gameType,
		ProviderID: providerID,
		Status:     SessionActive,
		StreamURL:  streamURL,
		Token:      token,
		CreatedAt:  time.Now(),
		ExpiresAt:  expiresAt,
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO casino_sessions (id, user_id, game_type, provider_id, status, stream_url, token, created_at, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		session.ID, session.UserID, session.GameType, session.ProviderID,
		session.Status, session.StreamURL, session.Token, session.CreatedAt, session.ExpiresAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	// Cache session in Redis for fast JWT validation
	s.redis.Set(ctx, fmt.Sprintf("casino:session:%s", sessionID), token, 4*time.Hour)

	s.logger.InfoContext(ctx, "casino session created",
		"session_id", sessionID, "user_id", userID, "game", gameType, "provider", providerID)

	return session, nil
}

// checkResponsibleGambling blocks casino launch if the user is currently
// self-excluded or in a cooling-off period. Fails closed: a real DB error
// (anything except "no row" / "table doesn't exist") rejects the launch
// rather than silently letting an excluded user in.
func (s *Service) checkResponsibleGambling(ctx context.Context, userID int64) error {
	var (
		excludedUntil   sql.NullTime
		coolingOffUntil sql.NullTime
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT self_excluded_until, cooling_off_until
		   FROM responsible_gambling WHERE user_id = $1`, userID,
	).Scan(&excludedUntil, &coolingOffUntil)
	if err == sql.ErrNoRows {
		return nil // no limits set yet — allowed
	}
	if err != nil {
		// Tolerate the table being absent in the monolith schema; otherwise
		// fail closed on a real query error.
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "does not exist") || strings.Contains(msg, "undefined_table") {
			return nil
		}
		s.logger.WarnContext(ctx, "responsible-gambling check failed", "user_id", userID, "error", err)
		return fmt.Errorf("could not verify responsible-gambling state")
	}
	now := time.Now()
	if excludedUntil.Valid && excludedUntil.Time.After(now) {
		return fmt.Errorf("account is self-excluded until %s", excludedUntil.Time.Format(time.RFC3339))
	}
	if coolingOffUntil.Valid && coolingOffUntil.Time.After(now) {
		return fmt.Errorf("account is in cooling-off period until %s", coolingOffUntil.Time.Format(time.RFC3339))
	}
	return nil
}

// checkKYCVerified blocks casino launch unless the user has completed KYC.
// Fails closed on DB error. Tolerates the column being absent on the
// monolith's lighter schema (returns nil — KYC enforcement is then handled
// by the monolith's own withdraw path).
func (s *Service) checkKYCVerified(ctx context.Context, userID int64) error {
	var status sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT kyc_status FROM users WHERE id = $1`, userID,
	).Scan(&status)
	if err == sql.ErrNoRows {
		return fmt.Errorf("user not found")
	}
	if err != nil {
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "does not exist") || strings.Contains(msg, "undefined_column") {
			return nil
		}
		s.logger.WarnContext(ctx, "kyc check failed", "user_id", userID, "error", err)
		return fmt.Errorf("could not verify KYC status")
	}
	if !status.Valid || status.String != "verified" {
		return fmt.Errorf("KYC verification required to launch casino games")
	}
	return nil
}

func (s *Service) ValidateSession(ctx context.Context, sessionID, token string) (*CasinoSession, error) {
	// Check Redis first
	cachedToken, err := s.redis.Get(ctx, fmt.Sprintf("casino:session:%s", sessionID)).Result()
	if err == nil && subtle.ConstantTimeCompare([]byte(cachedToken), []byte(token)) == 1 {
		// Fast path: token valid, fetch session details
		var session CasinoSession
		err := s.db.QueryRowContext(ctx,
			`SELECT id, user_id, game_type, provider_id, status, stream_url, token, created_at, expires_at
			 FROM casino_sessions WHERE id = $1 AND status = 'active'`,
			sessionID,
		).Scan(&session.ID, &session.UserID, &session.GameType, &session.ProviderID,
			&session.Status, &session.StreamURL, &session.Token, &session.CreatedAt, &session.ExpiresAt)
		if err != nil {
			return nil, fmt.Errorf("session not found")
		}

		if time.Now().After(session.ExpiresAt) {
			s.CloseSession(ctx, sessionID)
			return nil, fmt.Errorf("session expired")
		}

		return &session, nil
	}

	return nil, fmt.Errorf("invalid session token")
}

func (s *Service) CloseSession(ctx context.Context, sessionID string) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE casino_sessions SET status = 'closed' WHERE id = $1 AND status = 'active'", sessionID)
	if err != nil {
		return fmt.Errorf("close session: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("close session: rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("session %s is not active or does not exist", sessionID)
	}

	s.redis.Del(ctx, fmt.Sprintf("casino:session:%s", sessionID))
	return nil
}

// HandleSettlementWebhook processes settlement callbacks from casino providers.
// Idempotent via (session_id, round_id) unique constraint using INSERT ... ON CONFLICT DO NOTHING.
// The webhook is authenticated via HMAC signature at the handler level.
func (s *Service) HandleSettlementWebhook(ctx context.Context, sessionID, roundID string, stake, payout float64) error {
	// Look up the session directly -- webhook authentication is handled by HMAC signature verification
	var session CasinoSession
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, game_type, provider_id, status FROM casino_sessions WHERE id = $1`,
		sessionID,
	).Scan(&session.ID, &session.UserID, &session.GameType, &session.ProviderID, &session.Status)
	if err != nil {
		return fmt.Errorf("session not found")
	}

	betID := generateSessionID()
	pnl := payout - stake

	// Wrap the casino bet INSERT and wallet settlement in a single
	// serializable transaction with retry logic. The INSERT uses
	// ON CONFLICT (session_id, round_id) DO NOTHING for atomic idempotency,
	// eliminating the TOCTOU race from the previous SELECT EXISTS + INSERT.
	err = withSerializableRetry(ctx, s.db, 3, func(dbTx *sql.Tx) error {
		res, err := dbTx.ExecContext(ctx,
			`INSERT INTO casino_bets (id, session_id, user_id, game_type, round_id, stake, payout, status, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, 'settled', NOW())
			 ON CONFLICT (session_id, round_id) DO NOTHING`,
			betID, sessionID, session.UserID, session.GameType, roundID, stake, payout,
		)
		if err != nil {
			return fmt.Errorf("record casino bet: %w", err)
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("casino bet rows affected: %w", err)
		}
		if rows == 0 {
			// Already processed (idempotent duplicate) -- no error, no further processing.
			return nil
		}

		// Settle wallet within the same transaction so bet insert + wallet
		// update are atomic.
		if err := s.wallet.SettleBetTx(ctx, dbTx, session.UserID, betID, pnl, 0); err != nil {
			return fmt.Errorf("settle casino bet: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	s.logger.InfoContext(ctx, "casino bet settled",
		"session", sessionID, "round", roundID, "stake", stake, "payout", payout, "pnl", pnl)

	return nil
}

func (s *Service) GetSessionHistory(ctx context.Context, userID int64, limit int) ([]*CasinoSession, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, game_type, provider_id, status, stream_url, created_at, expires_at
		 FROM casino_sessions WHERE user_id = $1
		 ORDER BY created_at DESC LIMIT $2`,
		userID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*CasinoSession
	for rows.Next() {
		cs := &CasinoSession{}
		if err := rows.Scan(&cs.ID, &cs.UserID, &cs.GameType, &cs.ProviderID,
			&cs.Status, &cs.StreamURL, &cs.CreatedAt, &cs.ExpiresAt); err != nil {
			return nil, err
		}
		sessions = append(sessions, cs)
	}
	return sessions, rows.Err()
}

// GetSessionOwner returns the user_id that owns the given session.
func (s *Service) GetSessionOwner(ctx context.Context, sessionID string) (int64, error) {
	var userID int64
	err := s.db.QueryRowContext(ctx,
		"SELECT user_id FROM casino_sessions WHERE id = $1", sessionID,
	).Scan(&userID)
	if err != nil {
		return 0, fmt.Errorf("session not found")
	}
	return userID, nil
}

func generateSessionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand.Read failed: %v", err))
	}
	return hex.EncodeToString(b)
}

func generateToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand.Read failed: %v", err))
	}
	return hex.EncodeToString(b)
}
