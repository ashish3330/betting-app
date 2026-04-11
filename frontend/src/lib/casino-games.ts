/**
 * Shared casino game catalog.
 * Both `/casino` and `/casino/[category]` import from here to avoid drift.
 *
 * Canonical category slugs: live_casino | virtual_sports | slots | crash | card
 * (Historical duplicates like `virtual`, `virtual_sports`, `card_games`,
 * `crash_games` are handled via CATEGORY_ALIAS below.)
 */

export type GameBadge = "NEW" | "HOT" | null;

export type GameCategory =
  | "live_casino"
  | "virtual_sports"
  | "slots"
  | "crash"
  | "card";

export interface CasinoGameItem {
  id: string;
  name: string;
  type: string;
  provider_id: string;
  provider_name: string;
  is_live: boolean;
  image?: string | null;
  icon: string;
  category: GameCategory;
  badge?: GameBadge;
  min_bet?: number;
}

/** Canonicalise legacy / duplicate category slugs to the modern ones. */
export const CATEGORY_ALIAS: Record<string, GameCategory> = {
  live_casino: "live_casino",
  virtual: "virtual_sports",
  virtual_sports: "virtual_sports",
  slots: "slots",
  crash: "crash",
  crash_games: "crash",
  card: "card",
  card_games: "card",
};

export function canonicalCategory(slug: string | undefined | null): GameCategory | "all" {
  if (!slug || slug === "all") return "all";
  return CATEGORY_ALIAS[slug] ?? (slug as GameCategory);
}

export const CASINO_CATEGORIES: { id: GameCategory | "all"; name: string }[] = [
  { id: "all", name: "All Games" },
  { id: "live_casino", name: "Live Casino" },
  { id: "virtual_sports", name: "Virtual Sports" },
  { id: "slots", name: "Slots" },
  { id: "crash", name: "Crash Games" },
  { id: "card", name: "Card Games" },
];

export const CASINO_PROVIDERS = [
  { id: "all", name: "All Providers" },
  { id: "evolution", name: "Evolution" },
  { id: "ezugi", name: "Ezugi" },
  { id: "betgames", name: "BetGames" },
  { id: "superspade", name: "Super Spade" },
  { id: "tvbet", name: "TVBet" },
];

export const CASINO_CATEGORY_META: Record<GameCategory, { title: string; description: string }> = {
  live_casino: {
    title: "Live Casino",
    description: "Play with real dealers in real-time.",
  },
  virtual_sports: {
    title: "Virtual Sports",
    description: "Fast-paced virtual sports with instant results.",
  },
  slots: {
    title: "Slots",
    description: "Spin the reels on premium slot machines.",
  },
  crash: {
    title: "Crash Games",
    description: "Cash out before the crash.",
  },
  card: {
    title: "Card Games",
    description: "Classic card games. Poker, Hi-Lo, 32 Card Casino.",
  },
};

export const CASINO_GAMES: CasinoGameItem[] = [
  // Live Casino
  { id: "teen-patti", name: "Teen Patti", type: "teen_patti", provider_id: "ezugi", provider_name: "Ezugi", is_live: true, image: null, icon: "TP", category: "live_casino", badge: "HOT", min_bet: 100 },
  { id: "andar-bahar", name: "Andar Bahar", type: "andar_bahar", provider_id: "evolution", provider_name: "Evolution", is_live: true, image: null, icon: "AB", category: "live_casino", badge: "HOT", min_bet: 100 },
  { id: "dragon-tiger", name: "Dragon Tiger", type: "dragon_tiger", provider_id: "ezugi", provider_name: "Ezugi", is_live: true, image: null, icon: "DT", category: "live_casino", badge: null, min_bet: 50 },
  { id: "roulette", name: "Auto Roulette", type: "roulette", provider_id: "evolution", provider_name: "Evolution", is_live: true, image: null, icon: "RO", category: "live_casino", badge: null, min_bet: 50 },
  { id: "baccarat", name: "Baccarat", type: "baccarat", provider_id: "evolution", provider_name: "Evolution", is_live: true, image: null, icon: "BA", category: "live_casino", badge: null, min_bet: 100 },
  { id: "blackjack-vip", name: "Blackjack VIP", type: "blackjack", provider_id: "evolution", provider_name: "Evolution", is_live: true, image: null, icon: "BJ", category: "live_casino", badge: null, min_bet: 500 },
  { id: "lucky7", name: "Lucky 7", type: "lucky7", provider_id: "betgames", provider_name: "BetGames", is_live: true, image: null, icon: "L7", category: "live_casino", badge: null, min_bet: 50 },

  // Card Games
  { id: "32-card", name: "32 Card Casino", type: "32_card", provider_id: "ezugi", provider_name: "Ezugi", is_live: true, image: null, icon: "32", category: "card", badge: "NEW", min_bet: 50 },
  { id: "poker", name: "Casino Hold'em", type: "poker", provider_id: "evolution", provider_name: "Evolution", is_live: true, image: null, icon: "PK", category: "card", badge: null, min_bet: 200 },
  { id: "hi-lo", name: "Hi-Lo", type: "hi_lo", provider_id: "ezugi", provider_name: "Ezugi", is_live: true, image: null, icon: "HL", category: "card", badge: null, min_bet: 50 },
  { id: "3card-poker", name: "3 Card Poker", type: "3card_poker", provider_id: "evolution", provider_name: "Evolution", is_live: true, image: null, icon: "3P", category: "card", badge: null, min_bet: 100 },

  // Crash
  { id: "crash-aviator", name: "Aviator", type: "aviator", provider_id: "betgames", provider_name: "BetGames", is_live: false, image: null, icon: "AV", category: "crash", badge: "HOT", min_bet: 10 },
  { id: "crash-jetx", name: "JetX", type: "jetx", provider_id: "betgames", provider_name: "BetGames", is_live: false, image: null, icon: "JX", category: "crash", badge: "NEW", min_bet: 10 },
  { id: "crash-cashcrash", name: "Cash or Crash", type: "cash_crash", provider_id: "evolution", provider_name: "Evolution", is_live: true, image: null, icon: "CC", category: "crash", badge: null, min_bet: 50 },

  // Virtual Sports
  { id: "virtual-cricket", name: "Virtual Cricket", type: "virtual_cricket", provider_id: "tvbet", provider_name: "TVBet", is_live: false, image: null, icon: "VC", category: "virtual_sports", badge: "NEW", min_bet: 20 },
  { id: "virtual-football", name: "Virtual Football", type: "virtual_football", provider_id: "tvbet", provider_name: "TVBet", is_live: false, image: null, icon: "VF", category: "virtual_sports", badge: null, min_bet: 20 },
  { id: "virtual-horse", name: "Virtual Horse Racing", type: "virtual_horse", provider_id: "tvbet", provider_name: "TVBet", is_live: false, image: null, icon: "VH", category: "virtual_sports", badge: "NEW", min_bet: 20 },
  { id: "virtual-greyhound", name: "Virtual Greyhound", type: "virtual_greyhound", provider_id: "tvbet", provider_name: "TVBet", is_live: false, image: null, icon: "VG", category: "virtual_sports", badge: null, min_bet: 20 },

  // Slots
  { id: "slots-golden", name: "Golden Fortune", type: "slots_golden", provider_id: "superspade", provider_name: "Super Spade", is_live: false, image: null, icon: "GF", category: "slots", badge: null, min_bet: 10 },
  { id: "slots-treasure", name: "Treasure Hunt", type: "slots_treasure", provider_id: "superspade", provider_name: "Super Spade", is_live: false, image: null, icon: "TH", category: "slots", badge: "NEW", min_bet: 10 },
  { id: "slots-megamoolah", name: "Mega Moolah", type: "slot_mega", provider_id: "superspade", provider_name: "Super Spade", is_live: false, image: null, icon: "MM", category: "slots", badge: "HOT", min_bet: 10 },
  { id: "slots-bonanza", name: "Sweet Bonanza", type: "slot_bonanza", provider_id: "superspade", provider_name: "Super Spade", is_live: false, image: null, icon: "SB", category: "slots", badge: null, min_bet: 10 },
];

/**
 * Visual tile gradient + SVG glyph rendered inside each game card.
 * Keeping it as a tiny component so both casino list pages share identical visuals.
 */
export const CATEGORY_GRADIENT: Record<GameCategory, string> = {
  live_casino: "from-red-600/60 via-red-800/40 to-slate-900",
  virtual_sports: "from-blue-600/60 via-indigo-800/40 to-slate-900",
  slots: "from-purple-600/60 via-fuchsia-800/40 to-slate-900",
  crash: "from-orange-500/60 via-amber-800/40 to-slate-900",
  card: "from-emerald-600/60 via-green-800/40 to-slate-900",
};
