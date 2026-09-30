import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ME_KEY } from "@/features/auth/use-me";
import { logout } from "@/features/auth/session";
import { planKeys } from "@/features/plan/queries";
import { api, unwrap } from "@/lib/api/client";
import type { TargetsBody } from "./targets";

/** Saves the four daily targets. Today's rings and the plan's totals read them, so every plan query is refreshed. */
export function useUpdateTargets() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: TargetsBody) => unwrap(api.PATCH("/me", { body })),
    onSuccess: (user) => {
      queryClient.setQueryData(ME_KEY, user);
      void queryClient.invalidateQueries({ queryKey: planKeys.all });
    },
  });
}

/** Deletes the account for good, then ends the session. A sign-out that fails afterwards does not undo the deletion. */
export function useDeleteAccount() {
  return useMutation({
    mutationFn: async (): Promise<void> => {
      await unwrap(api.DELETE("/me"));
      await logout().catch(() => undefined);
    },
  });
}
