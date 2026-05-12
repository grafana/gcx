document.addEventListener("DOMContentLoaded", () => {
  const iframe = document.querySelector("iframe");

  iframe.addEventListener("load", () => {
    try {
      patchIframeHistory(iframe, handleParamsChange);
    } catch (e) {
      console.warn("[gcx] could not patch iframe history to sync params:", e);
    }
  });
});

// Listen to changes in the iframe's URL search params and sync them to the parent window's search params.
// This allows for dashboard variables to persist across refreshes
function handleParamsChange(newSearch, oldSearch, iframe) {
  const url = new URL(window.location.href);
  url.search = newSearch;
  history.replaceState(null, "", url); // replaceState avoids adding a new entry in the browser history for each variable change
}

// Patches the iframe's history methods to detect changes in the URL search parameters.
function patchIframeHistory(iframe, onParamsChange) {
  const iframeWindow = iframe.contentWindow;
  let lastSearch = iframeWindow.location.search;

  function onURLChange() {
    const search = iframeWindow.location.search;
    if (search !== lastSearch) {
      onParamsChange(search, lastSearch, iframe);
      lastSearch = search;
    }
  }

  const origPushState = iframeWindow.history.pushState.bind(
    iframeWindow.history,
  );
  iframeWindow.history.pushState = function (state, title, url) {
    origPushState(state, title, url);
    onURLChange();
  };

  const origReplaceState = iframeWindow.history.replaceState.bind(
    iframeWindow.history,
  );
  iframeWindow.history.replaceState = function (state, title, url) {
    origReplaceState(state, title, url);
    onURLChange();
  };

  iframeWindow.addEventListener("popstate", onURLChange);
}
