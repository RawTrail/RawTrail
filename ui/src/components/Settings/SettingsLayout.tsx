import type { ReactNode } from "react";
import { Link, useLocation } from "react-router";
import { useCan } from "../../permissions-core";
import { useProfile } from "../../profile-core";
import { Toggle } from "../ui/Toggle";
import { offeredSections } from "./sections";
import "./SettingsLayout.scss";

// SettingsLayout wraps the admin settings pages with a shared sub-navigation between the
// sections the operator can access (the design's Admin settings area). It is mounted by the
// router around each settings page, so the individual page components stay free of nav chrome
// (and their unit tests can render bare). Sections the operator lacks permission for are
// omitted; the server chokepoint remains the authority (ADR-0012). The section list is shared with AccountMenu, whose
// "Admin settings" link is the only way in: see ./sections.

export function SettingsLayout({ children }: { readonly children: ReactNode }) {
  const can = useCan();
  const { pathname } = useLocation();
  const { profile, showAdvanced, setShowAdvanced } = useProfile();
  const sections = offeredSections(can, showAdvanced);

  return (
    <div className="settings-layout">
      <nav className="settings-layout__nav" aria-label="Settings sections">
        {sections.map((s) => {
          const active = pathname === s.to || pathname.startsWith(s.to + "/");
          return (
            <Link
              key={s.to}
              to={s.to}
              className={active ? "settings-layout__link settings-layout__link--active" : "settings-layout__link"}
              aria-current={active ? "page" : undefined}
            >
              {s.label}
            </Link>
          );
        })}
        {profile === "laptop" && (
          <div className="settings-layout__advanced">
            <Toggle id="settings-show-advanced" label="Show advanced settings" checked={showAdvanced}
              onChange={(e) => { setShowAdvanced(e.target.checked); }} />
          </div>
        )}
      </nav>
      <div className="settings-layout__content">{children}</div>
    </div>
  );
}
