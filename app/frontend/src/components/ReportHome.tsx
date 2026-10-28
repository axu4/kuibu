import { useState } from "react";
import { ReportView } from "./ReportView";
import { ReportLibrary } from "./ReportLibrary";
import { SEView } from "./SEView";

import type { SettingsTab } from "./Settings";

interface ReportHomeProps {
  initialWeek?: string;
  initialPersonId?: string;
  initialLevel?: string;
  onOpenSettings?: (tab: SettingsTab) => void;
}

type HomeTab = "library" | "mine" | "se";

/**
 * 周报首页：周报库（默认）/ 我的组（DE：收组员周报、按项目分拣成组报）/
 * 项目汇总（SE：我当 SE 的项目，收各组交来的内容合并成项目周报）。
 */
export function ReportHome({ initialWeek, initialPersonId, initialLevel, onOpenSettings }: ReportHomeProps) {
  const [tab, setTab] = useState<HomeTab>(initialPersonId || initialWeek ? "mine" : "library");
  return (
    <div className="report-home">
      <div className="report-home-nav">
        <div className="segmented">
          <button type="button" className={`segmented-btn${tab === "library" ? " is-active" : ""}`} onClick={() => setTab("library")}>
            周报库
          </button>
          <button type="button" className={`segmented-btn${tab === "mine" ? " is-active" : ""}`} onClick={() => setTab("mine")}>
            我的组
          </button>
          <button type="button" className={`segmented-btn${tab === "se" ? " is-active" : ""}`} onClick={() => setTab("se")}>
            项目汇总
          </button>
        </div>
      </div>
      <div className="report-home-body">
        {tab === "library" && <ReportLibrary initialWeek={initialWeek} onOpenGroups={() => onOpenSettings?.("people")} />}
        {tab === "se" && <SEView mode="week" />}
        {tab === "mine" && (
          <ReportView
            memberOnly
            initialWeek={initialWeek}
            initialPersonId={initialPersonId}
            initialLevel={initialLevel}
          />
        )}
      </div>
    </div>
  );
}
