import http from 'k6/http';
import { check, sleep } from 'k6';

// Inside the cluster the Service is reachable by its DNS name
const BASE_URL = __ENV.GUESTBOOK_URL || 'http://guestbook:3000';

export const options = {
  scenarios: {
    // Arrival rate: k6 keeps sending N requests/s even when the app slows down,
    // unlike a fixed number of virtual users that wait for each response
    visitors: {
      executor: 'ramping-arrival-rate',
      startRate: 10,
      timeUnit: '1s',
      preAllocatedVUs: 50,
      maxVUs: 300,
      // Long steady phase: burn-rate alerts (exercise 5) need several minutes of traffic
      // to fire *and* to resolve; chaos experiments (exercise 6) need a steady load to compare with.
      stages: [
        { duration: '1m', target: 200 },  // ramp up
        { duration: '15m', target: 200 }, // sustained load: the HPA scales out
        { duration: '1m', target: 0 },    // ramp down
      ],
    },
  },
  // The test fails if the app degrades: this is what the HPA is supposed to prevent
  thresholds: {
    http_req_failed: ['rate<0.01'],
    http_req_duration: ['p(95)<500'],
    checks: ['rate>0.99'],
  },
};

// One visit = what the browser does: load the UI, then fetch the messages from Redis
export default function () {
  const page = http.get(`${BASE_URL}/`, { tags: { name: 'ui' } });
  check(page, { 'ui is 200': (r) => r.status === 200 });

  const messages = http.get(`${BASE_URL}/lrange/guestbook`, { tags: { name: 'lrange' } });
  check(messages, { 'lrange is 200': (r) => r.status === 200 });

  sleep(0.1);
}
