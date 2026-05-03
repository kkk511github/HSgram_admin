const assert = require("node:assert/strict");
const { updateSupportPollFailureState } = require("./support_polling");

function runSupportPollingBehaviorTest() {
  const statusText = { textContent: "" };
  let stopCount = 0;
  let retryDelay = 0;

  let failures = updateSupportPollFailureState({
    failures: 0,
    error: new Error("network down"),
    statusText,
    maxFailures: 3,
    retryDelayMs: 15000,
    stopPolling: () => {
      stopCount += 1;
    },
    scheduleRetry: (delayMs) => {
      retryDelay = delayMs;
    },
  });

  assert.equal(failures, 1);
  assert.match(statusText.textContent, /network down/);
  assert.equal(stopCount, 0);
  assert.equal(retryDelay, 0);

  failures = updateSupportPollFailureState({
    failures,
    error: new Error("still down"),
    statusText,
    maxFailures: 3,
    retryDelayMs: 15000,
    stopPolling: () => {
      stopCount += 1;
    },
    scheduleRetry: (delayMs) => {
      retryDelay = delayMs;
    },
  });

  assert.equal(failures, 2);
  assert.match(statusText.textContent, /still down/);
  assert.equal(stopCount, 0);
  assert.equal(retryDelay, 0);

  failures = updateSupportPollFailureState({
    failures,
    error: new Error("third failure"),
    statusText,
    maxFailures: 3,
    retryDelayMs: 15000,
    stopPolling: () => {
      stopCount += 1;
    },
    scheduleRetry: (delayMs) => {
      retryDelay = delayMs;
    },
  });

  assert.equal(failures, 3);
  assert.match(statusText.textContent, /third failure/);
  assert.equal(stopCount, 1);
  assert.equal(retryDelay, 15000);
}

runSupportPollingBehaviorTest();
