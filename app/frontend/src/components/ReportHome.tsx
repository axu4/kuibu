import { useState } from "react";
import { ReportView } from "./ReportView";
import { ReportLibrary } from "./ReportLibrary";
import { GroupManager } from "./GroupManager";

interface ReportHomeProps {
  initialWeek?: string;
  initialPersonId?: string;
  initialLevel?: string;
}

type HomeTab = "library" | "mine" | "groups";

/** 周报首页：周报库（默认）/ 我的组 / 组管理；不再有框架组长和 SE 两个固定 tab。 */
export function ReportHome({ initialWeek, initialPersonId, initialLevel }: ReportHomeProps) {
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
        <button type="button" className={tab === "groups" ? "is-on" : ""} onClick={() => setTab("groups")}>
          组管理
        </button>
      </div>
      <div className="report-home-body">
        {tab === "library" && <ReportLibrary initialWeek={initialWeek} onOpenGroups={() => setTab("groups")} />}
        {tab === "mine" && (
          <ReportView
            memberOnly
            initialWeek={initialWeek}
            initialPersonId={initialPersonId}
            initialLevel={initialLevel}
          />
        )}
        {tab === "groups" && <GroupManager />}
      </div>
    </div>
  );
}
