"use client";

import { useEffect, useState } from "react";
import { IconOrg, IconPlus } from "@/components/icons";
import { listMyOrganizations, type Organization } from "@/lib/api/organizations";

export default function Organizations() {
  const [orgs, setOrgs] = useState<Organization[] | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    let alive = true;
    listMyOrganizations()
      .then((res) => {
        if (alive) setOrgs(res.data?.organizations ?? []);
      })
      .catch((e) => {
        if (alive) {
          setOrgs([]);
          setError(e instanceof Error ? e.message : "Failed to load organizations");
        }
      });
    return () => {
      alive = false;
    };
  }, []);

  const rows = (orgs ?? []).map((o) => ({
    id: o.id,
    name: o.name,
    members: o.member_count ?? 0,
    role: o.my_role ?? "",
  }));

  return (
    <div className="flex-1 overflow-y-auto">
      <div className="mx-auto w-full max-w-[1200px] px-12 py-12">
        <div className="mb-10 flex items-end justify-between gap-6">
          <div>
            <div className="caption-uppercase mb-3 text-muted">Workspace</div>
            <h1 className="display-xl">Organizations</h1>
            <p className="mt-3 max-w-[520px] text-body">
              Shared spaces with their own knowledge bases and members.
            </p>
          </div>
          <button className="btn btn-primary">
            <IconPlus className="h-4 w-4" /> New organization
          </button>
        </div>

        {error && <p className="caption mb-6 text-error">{error}</p>}
        {orgs !== null && orgs.length === 0 && !error && (
          <p className="caption mb-6 text-muted-soft">No organizations yet.</p>
        )}

        <div className="card overflow-hidden">
          {rows.map((o, i) => (
            <div
              key={o.id}
              className={`flex items-center gap-4 px-5 py-4 ${
                i > 0 ? "border-t border-hairline" : ""
              }`}
            >
              <div className="flex h-9 w-9 items-center justify-center rounded-full bg-surface-strong text-ink">
                <IconOrg className="h-4 w-4" />
              </div>
              <div className="flex-1">
                <div className="text-[15px] font-medium text-ink">{o.name}</div>
                <div className="caption text-muted">{o.members} members</div>
              </div>
              {o.role && <span className="badge-pill">{o.role}</span>}
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
