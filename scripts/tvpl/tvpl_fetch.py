"""Fetch thuvienphapluat.vn (TVPL) pages with the Lightpanda browser.

TVPL sits behind Cloudflare; curl from the WeRAG server gets a managed
challenge, a plain `lightpanda fetch` gets the page. This module only ever
loads pages the way a browser does and STOPS when Cloudflare shows a
challenge or the site asks for a login (docs/legal-document-feed-spec.md
§9.5): it never retries a challenged page another way.

Pages are read without logging in: the document body (div.content1) and the
amendment notes (GetLqhlTip, the box shown when a reader clicks a
highlighted provision) are served to anonymous visitors.

Every fetched page is kept in a cache directory, so a rerun of the converter
does not load TVPL again.
"""

import fcntl
import hashlib
import json
import os
import re
import shutil
import subprocess
import tempfile
import time
import urllib.parse

BASE = "https://thuvienphapluat.vn"

# Seconds between two page loads (spec §9.5: at least 5 s).
MIN_INTERVAL = 5.0
# Seconds between two amendment-note requests made from inside a page.
TIP_INTERVAL_MS = 400

# The in-page script that loads the amendment note of every highlighted
# provision. TVPL highlights a changed Điều/khoản with
#   atmm=".lqhlTootip-<bookmark id>"
# and its own click handler GETs
#   /page/apicontroler.aspx?Action=GetLqhlTip&lawID=<LawID>&bookmarkID=<id>
# This script makes the same requests, one at a time, and writes the answers
# as JSON into <pre id="werag-tvpl-tips">. Lightpanda polls it as the
# --wait-script until it returns true.
_TIPS_SCRIPT = r"""
(function () {
  if (window.__weragTips) return window.__weragTips.done;
  // still on a Cloudflare check page: wait for the real page to replace it
  if (!document.querySelector('div.content1')) return false;
  var st = window.__weragTips = { done: false };
  var law = (typeof cLawID !== 'undefined') ? String(cLawID) : '';
  if (!law) { var lm = /-(\d+)\.aspx/.exec(location.pathname); law = lm ? lm[1] : ''; }
  var seen = {}, ids = [];
  document.querySelectorAll('[atmm]').forEach(function (el) {
    var m = /lqhlTootip-(\d+)/.exec(el.getAttribute('atmm') || '');
    if (m && !seen[m[1]]) { seen[m[1]] = 1; ids.push(m[1]); }
  });
  var out = { law_id: law, bookmarks: ids.length, tips: {}, errors: {} };
  if (!law) out.errors['*'] = 'no TVPL document id on the page';
  function finish() {
    var pre = document.createElement('pre');
    pre.id = 'werag-tvpl-tips';
    pre.textContent = JSON.stringify(out);
    document.body.appendChild(pre);
    st.done = true;
  }
  var i = 0;
  function next() {
    if (i >= ids.length || !law) { finish(); return; }
    var id = ids[i++];
    try {
      var r = new XMLHttpRequest();
      r.open('GET', '/page/apicontroler.aspx?Action=GetLqhlTip&lawID=' + encodeURIComponent(law) +
        '&bookmarkID=' + encodeURIComponent(id) + '&_=' + Date.now(), false);
      r.send();
      if (r.status === 200) out.tips[id] = r.responseText; else out.errors[id] = 'HTTP ' + r.status;
    } catch (e) { out.errors[id] = String(e); }
    setTimeout(next, __INTERVAL__);
  }
  next();
  return false;
})()
"""


# Logs in the way the site's own form does: CheckFullLogin answers "<ok>",
# "<er>" or a confirmation (e.g. the account is signed in elsewhere), after
# which the form posts action=Login; "<ok>" means signed in. The answer is
# written into <pre id="werag-tvpl-login">.
_LOGIN_SCRIPT = r"""
(function () {
  if (window.__weragLogin) return true;
  window.__weragLogin = 1;
  var u = __USER__, p = __PASS__;
  function post(action) {
    var r = new XMLHttpRequest();
    r.open('POST', '/page/ajaxcontroler.aspx', false);
    r.setRequestHeader('Content-Type', 'application/x-www-form-urlencoded; charset=UTF-8');
    r.setRequestHeader('X-Requested-With', 'XMLHttpRequest');
    r.send('&l_txtUser=' + encodeURIComponent(u) + '&l_txtPass=' + encodeURIComponent(p) + '&action=' + action);
    return r.status === 200 ? r.responseText : ('HTTP ' + r.status);
  }
  var out = { check: post('CheckFullLogin') };
  if (out.check !== '<ok>' && out.check !== '<er>' && out.check.indexOf('HTTP ') !== 0) out.login = post('Login');
  var pre = document.createElement('pre');
  pre.id = 'werag-tvpl-login';
  pre.textContent = JSON.stringify(out);
  document.body.appendChild(pre);
  return true;
})()
"""


# Ad and tracking hosts on TVPL pages: not needed to read a document.
_AD_URLS = ["*doubleclick*", "*googlesyndication*", "*googletagmanager*", "*google-analytics*",
            "*zmedia.vn*", "*aj1559.online*", "*facebook*", "*dmca.com*"]


class TVPLStop(Exception):
    """TVPL showed a Cloudflare challenge or a login wall: stop, do not retry."""


class TVPLError(Exception):
    """The page could not be loaded (network, timeout, browser error)."""


class TVPLIncomplete(TVPLError):
    """The page loaded but some amendment notes did not: nothing is pushed."""


def find_lightpanda(explicit=None):
    for c in (explicit, os.environ.get("LIGHTPANDA_BIN"), shutil.which("lightpanda")):
        if c and os.path.isfile(c) and os.access(c, os.X_OK):
            return c
    raise TVPLError("lightpanda binary not found: pass --lightpanda or set LIGHTPANDA_BIN "
                    "(https://github.com/lightpanda-io/browser/releases)")


def doc_id_of_url(url):
    """The TVPL LawID at the end of a document URL (…-427301.aspx)."""
    m = re.search(r"-(\d+)\.aspx", urllib.parse.urlsplit(url).path)
    return m.group(1) if m else ""


class Fetcher:
    def __init__(self, cache_dir, lightpanda=None, interval=MIN_INTERVAL, timeout=900, log=print):
        self.bin = find_lightpanda(lightpanda)
        self.cache_dir = cache_dir
        os.makedirs(cache_dir, mode=0o700, exist_ok=True)
        os.chmod(cache_dir, 0o700)  # holds the session cookies
        # the signed-in session (cookies), kept between runs; never the password
        self.cookie_jar = os.path.join(cache_dir, "session-cookies.json")
        # one run per cache dir: runs share the session and the pacing
        self._lock = open(os.path.join(cache_dir, ".lock"), "w")
        try:
            fcntl.flock(self._lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except OSError:
            self._lock.close()
            raise TVPLError(f"another TVPL run is using {cache_dir}; wait for it to finish")
        self.interval = max(float(interval), MIN_INTERVAL)
        self.timeout = timeout
        self.log = log
        self._last = 0.0

    # -- cache ---------------------------------------------------------
    def _cache_path(self, kind, key):
        h = hashlib.sha1(key.encode("utf-8")).hexdigest()[:16]
        return os.path.join(self.cache_dir, f"{kind}-{h}.json")

    def _cached(self, kind, key):
        p = self._cache_path(kind, key)
        if os.path.exists(p):
            with open(p, encoding="utf-8") as f:
                return json.load(f)
        return None

    def _store(self, kind, key, value):
        p = self._cache_path(kind, key)
        tmp = p + ".tmp"
        with open(tmp, "w", encoding="utf-8") as f:
            json.dump(value, f, ensure_ascii=False)
        os.replace(tmp, p)

    # -- browser -------------------------------------------------------
    def _pace(self):
        wait = self._last + self.interval - time.monotonic()
        if wait > 0:
            time.sleep(wait)
        self._last = time.monotonic()

    def _run(self, url, wait_script=None, wait_ms=20000, session=True, fresh_session=False):
        self._pace()
        args = [self.bin, "fetch", url, "--json", "--dump", "html"]
        if session:
            if os.path.exists(self.cookie_jar) and not fresh_session:
                args += ["--cookie", self.cookie_jar]
            args += ["--cookie-jar", self.cookie_jar]
        tmp = None
        if wait_script:
            # 0600 in the 0700 cache dir, deleted right after the run; the
            # sign-in script holds the password
            fd, tmp = tempfile.mkstemp(suffix=".js", dir=self.cache_dir)
            with os.fdopen(fd, "w", encoding="utf-8") as f:
                f.write(wait_script)
            args += ["--wait-script-file", tmp]
        # Wait for the network to settle so a Cloudflare JavaScript check can
        # finish the way it does in any browser ("load" ends on the check
        # page itself; "done" never comes on TVPL's ad-polling pages). The
        # wait script is checked after that and itself waits for the body.
        args += ["--wait-until", "networkalmostidle"]
        # the browser never talks to the LAN (the API push is urllib, not this)
        args += ["--block-private-networks"]
        for pat in _AD_URLS:
            args += ["--block-urls", pat]
        args += ["--wait-ms", str(wait_ms)]
        try:
            p = subprocess.run(args, capture_output=True, timeout=self.timeout)
        except subprocess.TimeoutExpired:
            raise TVPLError(f"lightpanda timed out after {self.timeout}s on {url}")
        finally:
            if tmp:
                os.unlink(tmp)
            if session and os.path.exists(self.cookie_jar):
                os.chmod(self.cookie_jar, 0o600)
        stderr = p.stderr.decode("utf-8", "replace")
        # a challenge first: whatever else went wrong, the run must stop
        if 'msg="bot challenge"' in stderr or "err=BotChallenge" in stderr:
            raise TVPLStop(f"Cloudflare challenge on {url}; stopping (do not retry another way)")
        try:
            res = json.loads(p.stdout.decode("utf-8", "replace"))
        except ValueError:
            tail = stderr.strip().splitlines()[-3:]
            raise TVPLError(f"lightpanda gave no JSON for {url} (exit {p.returncode}): {' | '.join(tail)}")
        if res.get("bot_challenge") or res.get("error") == "BotChallenge":
            raise TVPLStop(f"Cloudflare challenge on {url}; stopping (do not retry another way)")
        html = res.get("content") or ""
        status = res.get("http_status")
        if status != 200:
            raise TVPLError(f"HTTP {status} on {url}")
        if "<title>Just a moment" in html or "challenge-platform" in html[:5000]:
            raise TVPLStop(f"Cloudflare challenge page on {url}; stopping")
        return res.get("url") or url, html

    # -- pages ---------------------------------------------------------
    def login(self, user, password):
        """Sign in and keep the session cookies in the cache directory."""
        if not user or not password:
            raise TVPLError("TVPL_USER and TVPL_PASS are needed to sign in")
        self.log("  TVPL sign-in")
        script = (_LOGIN_SCRIPT.replace("__USER__", json.dumps(user))
                  .replace("__PASS__", json.dumps(password)))
        _, html = self._run(BASE + "/", wait_script=script, wait_ms=60000, fresh_session=True)
        m = re.search(r'<pre id="werag-tvpl-login">(.*?)</pre>', html, re.S)
        if not m:
            raise TVPLError("sign-in script did not run")
        import html as _html
        ans = json.loads(_html.unescape(m.group(1)))
        if ans.get("check") == "<ok>" or ans.get("login") == "<ok>":
            return True
        # the site's own message (wrong password, account locked …), no secrets
        raw = ans.get("login") or ans.get("check") or ""
        msg = " ".join(re.sub(r"<(?!er>)[^>]+>", " ", raw).split())[:200] or "(empty answer)"
        raise TVPLStop("TVPL sign-in failed: " + msg)

    def search(self, keyword, refresh=False):
        """HTML of the TVPL search page for a keyword (usually a số hiệu)."""
        url = BASE + "/page/tim-van-ban.aspx?" + urllib.parse.urlencode({"keyword": keyword})
        hit = None if refresh else self._cached("search", url)
        if hit:
            return hit["html"]
        self.log(f"  TVPL search: {keyword}")
        _, html = self._run(url)
        self._store("search", url, {"url": url, "html": html})
        return html

    def document(self, url, refresh=False):
        """(final url, page html, tips) of a document page.

        tips maps a bookmark id to the raw GetLqhlTip answer (HTML)."""
        key = doc_id_of_url(url) or url
        hit = None if refresh else self._cached("doc", key)
        if hit:
            return hit["url"], hit["html"], hit["tips"]
        self.log(f"  TVPL document: {url}")
        script = _TIPS_SCRIPT.replace("__INTERVAL__", str(TIP_INTERVAL_MS))
        # up to ~1800 highlighted provisions at 0.4 s each
        final, html = self._run(url, wait_script=script, wait_ms=self.timeout * 1000 - 60000)
        tips, errors = extract_tips(html)
        if 'class="content1"' not in html:
            if re.search(r'id="l_txtUser"|login\.aspx', html) and "Đăng nhập" in html:
                raise TVPLStop(f"TVPL page {url} has no document body (login wall?); stopping")
            raise TVPLError(f"TVPL page {url} has no div.content1")
        if errors:
            # not cached: a rerun loads the page again
            raise TVPLIncomplete(f"{len(errors)} amendment notes of {url} did not load: {list(errors.items())[:3]}")
        # highlighted provisions whose note is for members only
        locked = len(re.findall(r'class="clsBookmark clsopentLogin"', html))
        if locked and not tips:
            raise TVPLStop(f"TVPL hides the amendment notes of {url} from this session "
                           f"({locked} locked provisions): sign in (TVPL_USER/TVPL_PASS) and rerun")
        self._store("doc", key, {"url": final, "html": html, "tips": tips, "locked": locked})
        return final, html, tips


def extract_tips(html):
    """The tips JSON the in-page script left in the dumped page."""
    m = re.search(r'<pre id="werag-tvpl-tips">(.*?)</pre>', html, re.S)
    if not m:
        return {}, {"*": "tips block missing (page script did not finish in time)"}
    import html as _html
    data = json.loads(_html.unescape(m.group(1)))
    tips, errors = data.get("tips") or {}, data.get("errors") or {}
    want = data.get("bookmarks")
    if isinstance(want, int) and len(tips) + len(errors) < want:
        errors["*"] = f"only {len(tips)} of {want} notes loaded"
    return tips, errors
