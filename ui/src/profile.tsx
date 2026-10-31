import { useMemo, useState, type ReactNode } from "react";

import { ADVANCED_KEY, ProfileContext, type Profile } from "./profile-core";

// Components for the deployment profile; the context and hooks live in profile-core.ts so react-refresh fast-refresh works.

function storedChoice(): boolean {
  try {
    return localStorage.getItem(ADVANCED_KEY) === "1";
  } catch {
    // Storage blocked (a private window, cleared site data): the switch starts off and its choice is not kept.
    return false;
  }
}

function storeChoice(show: boolean): void {
  try {
    localStorage.setItem(ADVANCED_KEY, show ? "1" : "0");
  } catch {
    // As above: the choice holds for this page's life only.
  }
}

interface ProfileProviderProps {
  readonly profile: Profile;
  readonly children: ReactNode;
}

// ProfileProvider carries the profile and the advanced-settings switch to the console. On a server the switch is always on and has no
// effect; on a laptop it starts from the choice this browser kept, off when there is none.
export function ProfileProvider({ profile, children }: ProfileProviderProps) {
  const [chosen, setChosen] = useState(storedChoice);
  const value = useMemo(() => ({
    profile,
    showAdvanced: profile === "server" || chosen,
    setShowAdvanced: (show: boolean) => {
      setChosen(show);
      storeChoice(show);
    },
  }), [profile, chosen]);
  return <ProfileContext.Provider value={value}>{children}</ProfileContext.Provider>;
}
