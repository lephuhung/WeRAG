"use client";

import { useState } from "react";

const SECTIONS = [
  { id: "general", label: "General" },
  { id: "models", label: "Models" },
  { id: "retrieval", label: "Retrieval" },
  { id: "members", label: "Members" },
  { id: "api", label: "API keys" },
];

export default function Settings() {
  const [active, setActive] = useState("general");

  return (
    <div className="flex-1 overflow-y-auto">
      <div className="mx-auto w-full max-w-[1200px] px-12 py-12">
        <div className="caption-uppercase mb-3 text-muted">Workspace</div>
        <h1 className="display-xl mb-10">Settings</h1>

        <div className="flex gap-10">
          {/* section list */}
          <div className="w-[220px] shrink-0">
            {SECTIONS.map((s) => (
              <button
                key={s.id}
                onClick={() => setActive(s.id)}
                className={`nav-item mb-0.5 ${active === s.id ? "active" : ""}`}
              >
                {s.label}
              </button>
            ))}
          </div>

          {/* panel */}
          <div className="min-w-0 flex-1">
            <div className="card p-8">
              <h2 className="title-md mb-6">
                {SECTIONS.find((s) => s.id === active)?.label}
              </h2>
              <div className="flex max-w-[480px] flex-col gap-5">
                <label className="block">
                  <span className="caption mb-1.5 block text-muted">Workspace name</span>
                  <input className="input" defaultValue="WeRAG" />
                </label>
                <label className="block">
                  <span className="caption mb-1.5 block text-muted">Description</span>
                  <input className="input" placeholder="Optional description" />
                </label>
                <label className="block">
                  <span className="caption mb-1.5 block text-muted">Default language</span>
                  <input className="input" defaultValue="English" />
                </label>
                <div className="mt-2 flex gap-3">
                  <button className="btn btn-primary">Save changes</button>
                  <button className="btn btn-outline">Cancel</button>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
