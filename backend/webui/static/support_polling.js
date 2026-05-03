(function exposeSupportPolling(root, factory) {
  if (typeof module === "object" && module.exports) {
    module.exports = factory();
    return;
  }
  root.HSgramSupportPolling = factory();
})(typeof globalThis !== "undefined" ? globalThis : window, function buildSupportPolling() {
  function updateSupportPollFailureState(options) {
    const failures = Number(options.failures || 0) + 1;
    const error = options.error || {};
    const message = error.message ? error.message : "客服消息刷新失败";
    const statusText = options.statusText;
    if (statusText) {
      statusText.textContent = `客服消息刷新失败: ${message}`;
    }

    if (failures >= options.maxFailures) {
      options.stopPolling();
      options.scheduleRetry(options.retryDelayMs);
    }
    return failures;
  }

  return {
    updateSupportPollFailureState,
  };
});
