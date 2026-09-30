import { useEffect } from "react";

type Pageable = { hasNextPage: boolean; isFetchingNextPage: boolean; isFetchNextPageError: boolean; fetchNextPage: () => unknown };

/** Keeps asking for the next page until there is none. It stops once a page has failed, so a broken server is not hammered. */
export function useLoadAllPages({ hasNextPage, isFetchingNextPage, isFetchNextPageError, fetchNextPage }: Pageable): void {
  useEffect(() => {
    if (hasNextPage && !isFetchingNextPage && !isFetchNextPageError) void fetchNextPage();
  }, [hasNextPage, isFetchingNextPage, isFetchNextPageError, fetchNextPage]);
}
