import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo } from "react";
import { api, unwrap } from "@/lib/api/client";
import type { components } from "@/lib/api/schema.gen";

type Schemas = components["schemas"];
export type DietTemplate = Schemas["DietTemplate"];
export type DietTemplateSummary = Schemas["DietTemplateSummary"];
export type TemplateSlot = Schemas["TemplateSlot"];
export type TemplateSlotInput = Schemas["TemplateSlotInput"];
export type CreateDietTemplateRequest = Schemas["CreateDietTemplateRequest"];
export type UpdateDietTemplateRequest = Schemas["UpdateDietTemplateRequest"];

/** Per-resource keys. The list keys prefix nothing else, so refreshing lists never touches an open template. */
export const templateKeys = {
  mine: ["templates", "mine"] as const,
  partner: ["templates", "partner"] as const,
  detail: (id: string) => ["templates", "detail", id] as const,
};

export function useTemplates(scope: "mine" | "partner") {
  return useInfiniteQuery({
    queryKey: scope === "mine" ? templateKeys.mine : templateKeys.partner,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) => {
      const query = { cursor: pageParam, limit: 20 };
      return scope === "mine" ? unwrap(api.GET("/diet-templates", { params: { query } })) : unwrap(api.GET("/partner/diet-templates", { params: { query } }));
    },
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  });
}

export function useTemplate(id: string) {
  return useQuery({ queryKey: templateKeys.detail(id), queryFn: () => unwrap(api.GET("/diet-templates/{id}", { params: { path: { id } } })) });
}

const createTemplate = (body: CreateDietTemplateRequest) => unwrap(api.POST("/diet-templates", { body }));
const copyTemplate = (id: string) => unwrap(api.POST("/diet-templates/{id}/copy", { params: { path: { id } } }));
const removeTemplate = async (id: string): Promise<void> => {
  await unwrap(api.DELETE("/diet-templates/{id}", { params: { path: { id } } }));
};
const updateTemplate = (id: string, body: UpdateDietTemplateRequest) => unwrap(api.PATCH("/diet-templates/{id}", { params: { path: { id } }, body }));
const replaceTemplateSlots = (id: string, items: TemplateSlotInput[]) =>
  unwrap(api.PUT("/diet-templates/{id}/slots", { params: { path: { id } }, body: { items } }));

export function useCreateTemplate() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: createTemplate,
    onSuccess: (template) => {
      queryClient.setQueryData(templateKeys.detail(template.id), template);
      void queryClient.invalidateQueries({ queryKey: templateKeys.mine });
    },
  });
}

export function useCopyTemplate() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: copyTemplate,
    onSuccess: (template) => {
      queryClient.setQueryData(templateKeys.detail(template.id), template);
      void queryClient.invalidateQueries({ queryKey: templateKeys.mine });
    },
  });
}

export function useDeleteTemplate() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: removeTemplate,
    onSuccess: (_data, id) => {
      queryClient.removeQueries({ queryKey: templateKeys.detail(id) });
      void queryClient.invalidateQueries({ queryKey: templateKeys.mine });
    },
  });
}

/** The two writes the template editor's autosave makes. Each keeps the open template's cache on the server's latest answer. */
export function useTemplateActions(id: string) {
  const queryClient = useQueryClient();
  return useMemo(() => {
    const done = (template: DietTemplate) => {
      queryClient.setQueryData(templateKeys.detail(id), template);
      void queryClient.invalidateQueries({ queryKey: templateKeys.mine });
      return template;
    };
    return {
      patch: async (body: UpdateDietTemplateRequest) => done(await updateTemplate(id, body)),
      replaceSlots: async (items: TemplateSlotInput[]) => done(await replaceTemplateSlots(id, items)),
    };
  }, [queryClient, id]);
}
