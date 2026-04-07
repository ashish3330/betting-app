import http from 'k6/http';
import ws from 'k6/ws';
import { check, sleep } from 'k6';
import { Rate, Trend } from 'k6/metrics';

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const WS_URL = __ENV.WS_URL || 'ws://localhost:8080/ws';

// Custom metrics
const betPlacementLatency = new Trend('bet_placement_latency');
const betSuccessRate = new Rate('bet_success_rate');

export let options = {
  scenarios: {
    // Simulate steady state users browsing odds
    odds_viewers: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '1m', target: 1000 },
        { duration: '5m', target: 5000 },
        { duration: '2m', target: 5000 },
        { duration: '1m', target: 0 },
      ],
      exec: 'viewOdds',
    },
    // Simulate active bettors placing bets
    bettors: {
      executor: 'ramping-arrival-rate',
      startRate: 100,
      timeUnit: '1s',
      preAllocatedVUs: 500,
      maxVUs: 2000,
      stages: [
        { duration: '1m', target: 1000 },
        { duration: '5m', target: 5000 },
        { duration: '2m', target: 5000 },
        { duration: '1m', target: 0 },
      ],
      exec: 'placeBet',
    },
  },
  thresholds: {
    http_req_duration: ['p(95)<80', 'p(99)<200'],
    bet_placement_latency: ['p(95)<50'],
    bet_success_rate: ['rate>0.95'],
  },
};

// Login and get token
function getToken() {
  const loginRes = http.post(`${BASE_URL}/api/v1/auth/login`, JSON.stringify({
    username: `testuser_${__VU}`,
    password: 'testpassword123',
  }), { headers: { 'Content-Type': 'application/json' } });

  if (loginRes.status === 200) {
    return JSON.parse(loginRes.body).access_token;
  }
  return null;
}

export function viewOdds() {
  // Fetch market list
  const marketsRes = http.get(`${BASE_URL}/api/v1/markets?sport=cricket`);
  check(marketsRes, { 'markets loaded': (r) => r.status === 200 });

  // Fetch odds for first market
  if (marketsRes.status === 200) {
    const markets = JSON.parse(marketsRes.body);
    if (markets.length > 0) {
      const oddsRes = http.get(`${BASE_URL}/api/v1/markets/${markets[0].id}/odds`);
      check(oddsRes, { 'odds loaded': (r) => r.status === 200 || r.status === 404 });
    }
  }

  sleep(1);
}

export function placeBet() {
  const token = getToken();
  if (!token) {
    betSuccessRate.add(false);
    return;
  }

  const headers = {
    'Content-Type': 'application/json',
    'Authorization': `Bearer ${token}`,
  };

  const sides = ['back', 'lay'];
  const side = sides[Math.floor(Math.random() * sides.length)];
  const price = (1.5 + Math.random() * 3).toFixed(2);
  const stake = (10 + Math.random() * 990).toFixed(2);

  const start = Date.now();
  const betRes = http.post(`${BASE_URL}/api/v1/bet/place`, JSON.stringify({
    market_id: 'mock-ipl-match-001',
    selection_id: 1,
    side: side,
    price: parseFloat(price),
    stake: parseFloat(stake),
    client_ref: `k6-${__VU}-${__ITER}-${Date.now()}`,
  }), { headers });

  const duration = Date.now() - start;
  betPlacementLatency.add(duration);

  const success = betRes.status === 200;
  betSuccessRate.add(success);

  check(betRes, {
    'bet placed': (r) => r.status === 200,
    'bet latency < 80ms': () => duration < 80,
  });

  // Check orderbook after placing bet
  http.get(`${BASE_URL}/api/v1/market/mock-ipl-match-001/orderbook`, { headers });
}

export function websocketTest() {
  const token = getToken();
  if (!token) return;

  const res = ws.connect(WS_URL, {}, function(socket) {
    socket.on('open', function() {
      socket.send(JSON.stringify({
        type: 'auth',
        payload: { token: token },
      }));

      socket.send(JSON.stringify({
        type: 'subscribe',
        payload: { market_ids: ['mock-ipl-match-001', 'mock-ipl-fancy-001'] },
      }));
    });

    socket.on('message', function(msg) {
      const data = JSON.parse(msg);
      check(data, { 'received update': (d) => d.type !== undefined });
    });

    socket.setTimeout(function() {
      socket.close();
    }, 30000);
  });

  check(res, { 'ws status is 101': (r) => r && r.status === 101 });
}
