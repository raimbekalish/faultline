import { useEffect, useState } from "react";
import { Icon } from "./components";
import Overview from "./views/Overview";
import HistoryView from "./views/History";
import Detail from "./views/Detail";
import CompareView from "./views/Compare";
export default function App() {
  const [hash, setHash] = useState(window.location.hash || "#/");
  useEffect(() => {
    const change = () => setHash(window.location.hash || "#/");
    window.addEventListener("hashchange", change);
    return () => window.removeEventListener("hashchange", change);
  }, []);
  const [path, query = ""] = hash.slice(1).split("?"),
    params = new URLSearchParams(query),
    section =
      path.startsWith("/runs/") || path === "/history"
        ? "history"
        : path === "/compare"
          ? "compare"
          : "overview";
  useEffect(() => {
    document.title = `Faultline · ${section[0].toUpperCase() + section.slice(1)}`;
    window.scrollTo(0, 0);
  }, [hash, section]);
  return (
    <div className="app">
      <a
        href="#main"
        className="skip-link"
        onClick={(e) => {
          e.preventDefault();
          document.getElementById("main")?.focus();
        }}
      >
        Skip to content
      </a>
      <aside className="sidebar">
        <a className="brand" href="#/">
          <span className="brand-mark">
            <Icon name="pulse" />
          </span>
          faultline<span className="version">LOCAL</span>
        </a>
        <div className="workspace">
          <span className="workspace-icon">F</span>
          <div>
            Sample topology<small>Local workspace</small>
          </div>
          <span>⌄</span>
        </div>
        <span className="nav-label">ANALYZE</span>
        <nav aria-label="Main navigation">
          {[
            ["overview", "Overview", "#/"],
            ["history", "Experiment history", "#/history"],
            ["compare", "Compare runs", "#/compare"],
          ].map(([key, label, href]) => (
            <a
              key={key}
              href={href}
              className={section === key ? "active" : ""}
              aria-current={section === key ? "page" : undefined}
            >
              <Icon name={key} />
              {label}
              {section === key && <span className="nav-active-dot" />}
            </a>
          ))}
        </nav>
        <div className="sidebar-bottom">
          <span className="local-dot" />
          Your machine. Your evidence.
          <p>
            Local-first reliability testing
            <br />
            for distributed systems.
          </p>
          <div>
            READ-ONLY DASHBOARD <span>v0.1</span>
          </div>
        </div>
      </aside>
      <div className="main-shell">
        <header className="topbar">
          <div>
            <span className="breadcrumb">Workspace</span>
            <span>/</span>
            <strong>
              {section === "history"
                ? "Experiment history"
                : section === "compare"
                  ? "Compare runs"
                  : "Overview"}
            </strong>
          </div>
          <div>
            <span className="local-indicator">
              <i />
              Local SQLite
            </span>
            <span className="read-only">Read only</span>
          </div>
        </header>
        <main id="main" tabIndex={-1} key={hash}>
          {path.startsWith("/runs/") ? (
            <Detail id={path.slice(6)} />
          ) : path === "/history" ? (
            <HistoryView initialStatus={params.get("status") ?? ""} />
          ) : path === "/compare" ? (
            <CompareView
              initialLeft={params.get("left") ?? ""}
              initialRight={params.get("right") ?? ""}
            />
          ) : path === "/" || path === "" ? (
            <Overview />
          ) : (
            <div className="state">
              <h1>View not found</h1>
              <a href="#/">Return to overview</a>
            </div>
          )}
          <footer className="app-footer">
            <span>
              <Icon name="pulse" />
              Faultline · Evidence over assumptions.
            </span>
            <span>Local history · API v1</span>
          </footer>
        </main>
      </div>
    </div>
  );
}
