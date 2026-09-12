<script lang="ts">
  import {
    Background,
    BackgroundVariant,
    Controls,
    MarkerType,
    MiniMap,
    SvelteFlow,
    type Edge,
    type Node
  } from '@xyflow/svelte';
  import '@xyflow/svelte/dist/style.css';
  import type { FormSchema } from '../types';

  export let schema: FormSchema;
  export let selectedEdgeID = '';
  export let onSelectEdge: (id: string) => void;
  export let onPositionChange: (id: string, x: number, y: number) => void;

  let nodes: Node[] = [];
  let edges: Edge[] = [];

  $: rebuild(schema, selectedEdgeID);

  function rebuild(current: FormSchema, selected: string) {
    const metadata = (current.editor_metadata ?? {}) as Record<string, unknown>;
    const positions = (metadata.flow_positions ?? {}) as Record<string, { x?: number; y?: number }>;
    const pageNodes: Node[] = current.pages.map((page, index) => ({
      id: page.id,
      type: page.id === current.flow.start_page_id ? 'input' : 'default',
      position: {
        x: positions[page.id]?.x ?? 80 + (index % 3) * 260,
        y: positions[page.id]?.y ?? 70 + Math.floor(index / 3) * 180
      },
      data: { label: `${index + 1}. ${page.title}` },
      ariaLabel: `صفحهٔ ${page.title}`
    }));
    const endingNodes: Node[] = current.endings.map((ending, index) => ({
      id: ending.id,
      type: 'output',
      position: {
        x: positions[ending.id]?.x ?? 130 + index * 290,
        y: positions[ending.id]?.y ?? 420
      },
      data: { label: `پایان: ${ending.title}` },
      ariaLabel: `پایان ${ending.title}`,
      style: 'background:#eef9f6;border-color:#5ba89e'
    }));
    nodes = [...pageNodes, ...endingNodes];
    edges = current.flow.edges.map((edge) => ({
      id: edge.id,
      source: edge.source_id,
      target: edge.target_id,
      label: edge.fallback ? 'پیش‌فرض' : `شرط ${edge.priority}`,
      type: 'smoothstep',
      animated: edge.id === selected,
      markerEnd: { type: MarkerType.ArrowClosed },
      style: edge.id === selected ? 'stroke:#008d83;stroke-width:2.5' : undefined
    }));
  }
</script>

<div class="flow-canvas" aria-label="نمودار مسیرهای فرم">
  <SvelteFlow
    {nodes}
    {edges}
    fitView
    minZoom={0.35}
    maxZoom={1.7}
    nodesConnectable={false}
    onedgeclick={({ edge }) => onSelectEdge(edge.id)}
    onnodedragstop={({ targetNode }) => {
      if (targetNode) onPositionChange(targetNode.id, targetNode.position.x, targetNode.position.y);
    }}
  >
    <Controls position="bottom-left" />
    <MiniMap pannable zoomable />
    <Background variant={BackgroundVariant.Dots} gap={18} size={1.2} />
  </SvelteFlow>
</div>

<style>
  .flow-canvas {
    width: 100%;
    min-height: 520px;
    height: calc(100vh - 290px);
    border: 1px solid var(--line);
    border-radius: var(--radius);
    overflow: hidden;
    background: #fbfdff;
    direction: ltr;
  }

  :global(.svelte-flow__node) {
    min-width: 180px;
    border: 1px solid #b8c8d5;
    border-radius: 10px;
    padding: 0.8rem;
    background: white;
    box-shadow: var(--shadow-sm);
    color: var(--ink);
    font-family: 'Vazirmatn', Tahoma, sans-serif;
    direction: rtl;
  }

  :global(.svelte-flow__node-input) {
    border-color: var(--accent);
    box-shadow: 0 0 0 3px rgba(0, 141, 131, 0.1);
  }

  :global(.svelte-flow__edge-text) {
    font-family: 'Vazirmatn', Tahoma, sans-serif;
    font-size: 12px;
  }
</style>
