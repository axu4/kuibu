import { useState } from "react";
import { ReportView } from "./ReportView";
import { ReportLibrary } from "./ReportLibrary";

import type { SettingsTab } from "./Settings";

interface ReportHomeProps {
  initialWeek?: string;
  initialPersonId?: string;
  initialLevel?: string;
  onOpenSettings?: (tab: SettingsTab) => void;
}

type HomeTab = "library" | "mine";

/** 周报首页：周报库（默认）/ 我的组 / 组管理；不再有框架组长和 SE 两个固定 tab。 */
export function ReportHome({ initialWeek, initialPersonId, initialLevel, onOpenSettings }: ReportHomeProps) {
  const [tab, setTab] = useState<HomeTab>(initialPersonId || initialWeek ? "mine" : "library");
  return (
    <div className="report-home">
      <div className="report-home-nav">
        <button type="button" className={tab === "library" ? "is-on" : ""} onClick={() => setTab("library")}>
          周报库
        </button>
        <button type="button" className={tab === "mine" ? "is-on" : ""} onClick={() => setTab("mine")}>
          我的组
        </button>
      </div>
      <div className="report-home-body">
        {tab === "library" && <ReportLibrary initialWeek={initialWeek} onOpenGroups={() => onOpenSettings?.("people")} />}
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
