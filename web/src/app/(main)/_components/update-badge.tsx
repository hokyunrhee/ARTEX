"use client";

import * as React from "react";

import Link from "next/link";

import { ArrowUpCircleIcon } from "lucide-react";

import { api } from "@/lib/api";

/**
 * Top-bar "new version available" hint: queried once on full-page load; when there is an
 * update it lights up next to the version number, and clicking it jumps straight to the
 * "Version and updates" card on the system settings page.
 *
 * The backend caches its GitHub query result for 30 minutes, so querying once on every
 * mount here is safe -- the unauthenticated GitHub API allows only 60 requests/hour/IP, and
 * without that cache a few open tabs would burn through the quota, after which a genuine
 * update check would fail.
 *
 * Query failures are always silent: the top bar is not the place for errors; a user who
 * opens the settings page and clicks "Check for updates" will see the reason.
 */
export function UpdateBadge() {
  const [latest, setLatest] = React.useState("");

  React.useEffect(() => {
    let alive = true;
    api
      .checkUpdate()
      .then((r) => {
        // has_update already includes the "version numbers are comparable" check, so dev
        // builds will not light this hint.
        if (alive && r.has_update && r.latest) setLatest(r.latest.replace(/^v(?=\d)/, ""));
      })
      .catch(() => {
        // Silent: neither a missing network nor GitHub rate-limiting should pop an error
        // in the top bar.
      });
    return () => {
      alive = false;
    };
  }, []);

  if (!latest) return null;

  return (
    <Link
      href="/system/settings"
      title={`New version ${latest} available, click to go update`}
      className="inline-flex items-center gap-1.5 rounded-full bg-primary px-2.5 py-1 font-medium text-primary-foreground text-xs transition-opacity hover:opacity-90"
    >
      {/* Breathing dot: the top bar holds many elements and plain text is easy to miss, so
          the animation makes it stand out at a glance. */}
      <span className="relative flex size-1.5">
        <span className="absolute inline-flex size-full animate-ping rounded-full bg-primary-foreground opacity-75" />
        <span className="relative inline-flex size-1.5 rounded-full bg-primary-foreground" />
      </span>
      <ArrowUpCircleIcon className="size-3.5" />
      <span className="hidden sm:inline">New version {latest}</span>
      <span className="sm:hidden">New version</span>
    </Link>
  );
}
