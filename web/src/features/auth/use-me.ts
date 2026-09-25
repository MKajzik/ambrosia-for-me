import { useQuery } from "@tanstack/react-query";
import { api, unwrap } from "@/lib/api/client";

export const ME_KEY = ["me"] as const;

/** The signed-in user's profile. */
export function useMe() {
  return useQuery({ queryKey: ME_KEY, queryFn: () => unwrap(api.GET("/me")) });
}
