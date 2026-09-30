import { useMutation, useQueryClient } from "@tanstack/react-query";
import { PARTNER_KEY, mealKeys } from "@/features/meals/queries";
import { templateKeys } from "@/features/plan/template-queries";
import { shoppingKeys } from "@/features/shopping/queries";
import { api, unwrap } from "@/lib/api/client";

/** A new code replaces any pending one. The code is in this answer and nowhere else, ever. */
export function useCreateInvite() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => unwrap(api.POST("/partner/invite")),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: PARTNER_KEY }),
  });
}

export function useAcceptInvite() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (code: string) => unwrap(api.POST("/partner/accept", { body: { code } })),
    onSuccess: (partnership) => queryClient.setQueryData(PARTNER_KEY, partnership),
  });
}

/** Ends the link, or cancels a pending invite. Access ends at once, so what was the partner's is dropped from the cache too. */
export function useUnlinkPartner() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (): Promise<void> => {
      await unwrap(api.DELETE("/partner"));
    },
    onSuccess: () => {
      queryClient.setQueryData(PARTNER_KEY, null);
      for (const key of [mealKeys.partner, templateKeys.partner, shoppingKeys.partner]) queryClient.removeQueries({ queryKey: key });
    },
  });
}
