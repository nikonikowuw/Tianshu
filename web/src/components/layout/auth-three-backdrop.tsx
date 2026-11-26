import { useEffect, useRef } from 'react'
import {
  Color,
  Mesh,
  PerspectiveCamera,
  PlaneGeometry,
  Scene,
  ShaderMaterial,
  Vector2,
  WebGLRenderer,
} from 'three'

// Vertex-evaluated 3D Simplex Noise silk displacement + color bands (Stripe-style
// WebGL mesh architecture): keeps per-frame GPU cost tiny while hardware
// interpolation + per-pixel normal specular lighting deliver silky smooth folds.
const SILK_VERTEX_SHADER = /* glsl */ `
  uniform float uTime;
  uniform float uAspect;
  uniform vec2 uPointer;
  uniform float uPointerActive;
  uniform vec3 uColorBg;
  uniform vec3 uColorA;
  uniform vec3 uColorB;
  uniform vec3 uColorC;

  varying vec2 vUv;
  varying vec3 vColor;
  varying vec3 vNormal;
  varying float vElevation;
  varying float vPtrFalloff;

  // 3D Simplex Noise (Ashima Arts / Ian McEwan)
  vec4 permute(vec4 x) { return mod(((x * 34.0) + 1.0) * x, 289.0); }
  vec4 taylorInvSqrt(vec4 r) { return 1.79284291400159 - 0.85373472095314 * r; }

  float snoise(vec3 v) {
    const vec2 C = vec2(1.0 / 6.0, 1.0 / 3.0);
    const vec4 D = vec4(0.0, 0.5, 1.0, 2.0);

    vec3 i  = floor(v + dot(v, C.yyy));
    vec3 x0 = v - i + dot(i, C.xxx);

    vec3 g = step(x0.yzx, x0.xyz);
    vec3 l = 1.0 - g;
    vec3 i1 = min(g.xyz, l.zxy);
    vec3 i2 = max(g.xyz, l.zxy);

    vec3 x1 = x0 - i1 + C.xxx;
    vec3 x2 = x0 - i2 + C.yyy;
    vec3 x3 = x0 - D.yyy;

    i = mod(i, 289.0);
    vec4 p = permute(permute(permute(
              i.z + vec4(0.0, i1.z, i2.z, 1.0))
            + i.y + vec4(0.0, i1.y, i2.y, 1.0))
            + i.x + vec4(0.0, i1.x, i2.x, 1.0));

    float n_ = 1.0 / 7.0;
    vec3 ns = n_ * D.wyz - D.xzx;

    vec4 j = p - 49.0 * floor(p * ns.z * ns.z);

    vec4 x_ = floor(j * ns.z);
    vec4 y_ = floor(j - 7.0 * x_);

    vec4 x = x_ * ns.x + ns.yyyy;
    vec4 y = y_ * ns.x + ns.yyyy;
    vec4 h = 1.0 - abs(x) - abs(y);

    vec4 b0 = vec4(x.xy, y.xy);
    vec4 b1 = vec4(x.zw, y.zw);

    vec4 s0 = floor(b0) * 2.0 + 1.0;
    vec4 s1 = floor(b1) * 2.0 + 1.0;
    vec4 sh = -step(h, vec4(0.0));

    vec4 a0 = b0.xzyw + s0.xzyw * sh.xxyy;
    vec4 a1 = b1.xzyw + s1.xzyw * sh.zzww;

    vec3 p0 = vec3(a0.xy, h.x);
    vec3 p1 = vec3(a0.zw, h.y);
    vec3 p2 = vec3(a1.xy, h.z);
    vec3 p3 = vec3(a1.zw, h.w);

    vec4 norm = taylorInvSqrt(vec4(dot(p0, p0), dot(p1, p1), dot(p2, p2), dot(p3, p3)));
    p0 *= norm.x;
    p1 *= norm.y;
    p2 *= norm.z;
    p3 *= norm.w;

    vec4 m = max(0.6 - vec4(dot(x0, x0), dot(x1, x1), dot(x2, x2), dot(x3, x3)), 0.0);
    m = m * m;
    return 42.0 * dot(m * m, vec4(dot(p0, x0), dot(p1, x1), dot(p2, x2), dot(p3, x3)));
  }

  vec3 evaluateField(vec2 uv) {
    vec2 p = (uv - 0.5) * vec2(uAspect, 1.0);
    float t = uTime * 0.13;

    // Soft pointer magnetic pull
    vec2 ptr = uPointer * vec2(uAspect * 0.5, 0.5);
    float ptrFalloff = exp(-dot(p - ptr, p - ptr) * 2.1) * uPointerActive;
    vec2 wp = p - (p - ptr) * ptrFalloff * 0.16;

    // Flowing organic silk currents (3 layered noise octaves)
    float n1 = snoise(vec3(wp * 0.95 + vec2(t * 0.42, -t * 0.26), t * 0.55));
    float n2 = snoise(vec3(wp * 1.45 + vec2(-t * 0.31, t * 0.35) + n1 * 0.55, t * 0.45 + 4.1));
    float n3 = snoise(vec3(wp * 1.15 + vec2(n2 * 0.65, -n1 * 0.48) + vec2(2.7, 7.3), t * 0.38));

    float ribbon = sin(wp.x * 2.0 + wp.y * 1.25 + n1 * 1.85 + uTime * 0.22) * 0.12;
    float height = n1 * 0.22 + n2 * 0.10 + ribbon + ptrFalloff * 0.18;

    return vec3(height, n2, n3);
  }

  void main() {
    vUv = uv;

    vec2 p = (uv - 0.5) * vec2(uAspect, 1.0);
    vec2 ptr = uPointer * vec2(uAspect * 0.5, 0.5);
    vPtrFalloff = exp(-dot(p - ptr, p - ptr) * 2.1) * uPointerActive;

    float eps = 0.022;
    vec3 fC = evaluateField(uv);
    float hR = evaluateField(uv + vec2(eps, 0.0)).x;
    float hU = evaluateField(uv + vec2(0.0, eps)).x;

    vec3 pos = position;
    pos.z += fC.x;

    // Smooth analytic normal from displaced silk surface
    vNormal = normalize(vec3((fC.x - hR) / eps * 0.36, (fC.x - hU) / eps * 0.36, 1.0));
    vElevation = fC.x;

    // Smooth multi-stop silk aurora color layers
    float bandA = smoothstep(-0.55, 0.68, fC.y + p.x * 0.25 + 0.12);
    float bandB = smoothstep(-0.52, 0.72, fC.z - p.y * 0.38 + fC.x * 0.85);
    float bandC = smoothstep(-0.28, 0.82, fC.y * 0.55 - fC.z * 0.55 + p.x * 0.18);

    vec3 col = mix(uColorBg, uColorA, bandA * 0.90);
    col = mix(col, uColorB, bandB * 0.82);
    col = mix(col, uColorC, bandC * 0.66);
    vColor = col;

    gl_Position = projectionMatrix * modelViewMatrix * vec4(pos, 1.0);
  }
`

const SILK_FRAGMENT_SHADER = /* glsl */ `
  uniform float uTime;
  uniform vec2 uPointer;
  uniform vec3 uColorBg;
  uniform vec3 uColorHighlight;
  uniform float uSheenStrength;
  uniform float uGrainStrength;
  uniform float uVignetteStrength;

  varying vec2 vUv;
  varying vec3 vColor;
  varying vec3 vNormal;
  varying float vElevation;
  varying float vPtrFalloff;

  float hash12(vec2 p) {
    vec3 p3 = fract(vec3(p.xyx) * 0.1031);
    p3 += dot(p3, p3.yzx + 33.33);
    return fract((p3.x + p3.y) * p3.z);
  }

  void main() {
    vec3 col = vColor;

    // Per-pixel Blinn-Phong satin sheen gliding over 3D silk folds
    vec3 N = normalize(vNormal);
    vec3 L1 = normalize(vec3(-0.45 + uPointer.x * 0.28, 0.62 + uPointer.y * 0.22, 0.86));
    vec3 L2 = normalize(vec3(0.68, -0.36, 0.72));
    vec3 V = vec3(0.0, 0.0, 1.0);
    vec3 H1 = normalize(L1 + V);
    vec3 H2 = normalize(L2 + V);

    float spec1 = pow(max(dot(N, H1), 0.0), 12.0);
    float spec2 = pow(max(dot(N, H2), 0.0), 9.0) * 0.55;
    float crest = smoothstep(-0.18, 0.34, vElevation);
    float sheen = (spec1 + spec2) * (0.42 + 0.58 * crest) + vPtrFalloff * 0.22;

    col = mix(col, uColorHighlight, clamp(sheen * uSheenStrength, 0.0, 0.76));

    // Soft atmospheric vignette keeping focus gently centered
    float distCenter = length((vUv - 0.5) * vec2(1.15, 1.0));
    float vignette = smoothstep(0.32, 1.15, distCenter);
    col = mix(col, uColorBg, vignette * uVignetteStrength);

    // Fine optical film grain eliminates 8-bit gradient banding
    float grain = (hash12(gl_FragCoord.xy + fract(uTime * 0.13) * 17.0) - 0.5) * uGrainStrength;
    col += grain;

    gl_FragColor = vec4(clamp(col, 0.0, 1.0), 1.0);
  }
`

function isDarkTheme(): boolean {
  return document.documentElement.classList.contains('dark')
}

// Curated Apple / Stripe-inspired silk palettes aligned with slate theme tokens.
const LIGHT_PALETTE = {
  bg: new Color('#f6f8fc'),
  colorA: new Color('#c4ddff'), // celestial sky mist
  colorB: new Color('#dbd2fe'), // soft periwinkle-lavender
  colorC: new Color('#c9f2f7'), // glacial pearl aqua
  highlight: new Color('#ffffff'), // sunlit pearl silk crest
  sheenStrength: 0.64,
  grainStrength: 0.015,
  vignetteStrength: 0.24,
}

const DARK_PALETTE = {
  bg: new Color('#070b16'),
  colorA: new Color('#163170'), // deep sapphire current
  colorB: new Color('#382075'), // velvety twilight indigo
  colorC: new Color('#0c5c7a'), // aurora glacial teal-blue
  highlight: new Color('#93c5fd'), // luminous ice-silver silk sheen
  sheenStrength: 0.5,
  grainStrength: 0.022,
  vignetteStrength: 0.4,
}

export default function AuthThreeBackdrop() {
  const containerRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    const container = containerRef.current
    if (!container) return

    let renderer: WebGLRenderer
    try {
      renderer = new WebGLRenderer({
        antialias: true,
        alpha: false,
        powerPreference: 'high-performance',
      })
    } catch {
      return
    }

    const initialWidth = container.clientWidth || window.innerWidth || 1280
    const initialHeight = container.clientHeight || window.innerHeight || 800
    // Soft ambient silk looks identical at 1.5x while cutting GPU fill-rate cleanly.
    const pixelRatio = Math.min(window.devicePixelRatio || 1, 1.5)

    renderer.setPixelRatio(pixelRatio)
    renderer.setSize(initialWidth, initialHeight)
    renderer.domElement.style.display = 'block'
    renderer.domElement.style.width = '100%'
    renderer.domElement.style.height = '100%'
    container.appendChild(renderer.domElement)

    const scene = new Scene()
    const fov = 40
    const camZ = 2.6
    const camera = new PerspectiveCamera(
      fov,
      initialWidth / Math.max(1, initialHeight),
      0.1,
      20
    )
    camera.position.set(0, 0, camZ)

    const initialDark = isDarkTheme()
    let themeTarget = initialDark ? 1 : 0
    let themeMix = themeTarget
    const startPalette = initialDark ? DARK_PALETTE : LIGHT_PALETTE

    const geometry = new PlaneGeometry(1, 1, 64, 48)
    const material = new ShaderMaterial({
      vertexShader: SILK_VERTEX_SHADER,
      fragmentShader: SILK_FRAGMENT_SHADER,
      uniforms: {
        uTime: { value: 0 },
        uAspect: { value: initialWidth / Math.max(1, initialHeight) },
        uPointer: { value: new Vector2(0, 0) },
        uPointerActive: { value: 0 },
        uColorBg: { value: startPalette.bg.clone() },
        uColorA: { value: startPalette.colorA.clone() },
        uColorB: { value: startPalette.colorB.clone() },
        uColorC: { value: startPalette.colorC.clone() },
        uColorHighlight: { value: startPalette.highlight.clone() },
        uSheenStrength: { value: startPalette.sheenStrength },
        uGrainStrength: { value: startPalette.grainStrength },
        uVignetteStrength: { value: startPalette.vignetteStrength },
      },
    })

    const mesh = new Mesh(geometry, material)
    scene.add(mesh)

    const fitMeshToViewport = (w: number, h: number) => {
      const aspect = w / Math.max(1, h)
      camera.aspect = aspect
      camera.updateProjectionMatrix()
      const visibleHeight = 2 * camZ * Math.tan(((fov * Math.PI) / 180) * 0.5)
      const visibleWidth = visibleHeight * aspect
      // Overscan 30% so displaced 3D edges never expose viewport corners
      mesh.scale.set(visibleWidth * 1.3, visibleHeight * 1.3, 1)
      material.uniforms.uAspect.value = aspect
    }

    fitMeshToViewport(initialWidth, initialHeight)

    const applyThemeMix = (mix: number) => {
      material.uniforms.uColorBg.value
        .copy(LIGHT_PALETTE.bg)
        .lerp(DARK_PALETTE.bg, mix)
      material.uniforms.uColorA.value
        .copy(LIGHT_PALETTE.colorA)
        .lerp(DARK_PALETTE.colorA, mix)
      material.uniforms.uColorB.value
        .copy(LIGHT_PALETTE.colorB)
        .lerp(DARK_PALETTE.colorB, mix)
      material.uniforms.uColorC.value
        .copy(LIGHT_PALETTE.colorC)
        .lerp(DARK_PALETTE.colorC, mix)
      material.uniforms.uColorHighlight.value
        .copy(LIGHT_PALETTE.highlight)
        .lerp(DARK_PALETTE.highlight, mix)
      material.uniforms.uSheenStrength.value =
        LIGHT_PALETTE.sheenStrength +
        (DARK_PALETTE.sheenStrength - LIGHT_PALETTE.sheenStrength) * mix
      material.uniforms.uGrainStrength.value =
        LIGHT_PALETTE.grainStrength +
        (DARK_PALETTE.grainStrength - LIGHT_PALETTE.grainStrength) * mix
      material.uniforms.uVignetteStrength.value =
        LIGHT_PALETTE.vignetteStrength +
        (DARK_PALETTE.vignetteStrength - LIGHT_PALETTE.vignetteStrength) * mix
    }

    // ── Pointer interaction state ──────────────────────────────────────────
    let targetX = 0
    let targetY = 0
    let currentX = 0
    let currentY = 0
    let pointerActiveTarget = 0
    let pointerActiveCurrent = 0

    const handlePointerMove = (e: PointerEvent) => {
      const rect = container.getBoundingClientRect()
      if (rect.width <= 0 || rect.height <= 0) return
      const nx = ((e.clientX - rect.left) / rect.width) * 2 - 1
      const ny = -(((e.clientY - rect.top) / rect.height) * 2 - 1)
      targetX = Math.max(-1, Math.min(1, nx))
      targetY = Math.max(-1, Math.min(1, ny))
      pointerActiveTarget = 1
    }

    const handlePointerLeave = () => {
      targetX = 0
      targetY = 0
      pointerActiveTarget = 0
    }

    window.addEventListener('pointermove', handlePointerMove, { passive: true })
    document.addEventListener('pointerleave', handlePointerLeave)
    window.addEventListener('blur', handlePointerLeave)

    // ── Observers & lifecycle ──────────────────────────────────────────────
    const motionQuery = window.matchMedia?.('(prefers-reduced-motion: reduce)')
    let reducedMotion = motionQuery?.matches ?? false
    let contextLost = false

    const renderStaticFrame = () => {
      if (contextLost || document.hidden) return
      renderer.render(scene, camera)
    }

    const themeObserver = new MutationObserver(() => {
      themeTarget = isDarkTheme() ? 1 : 0
      if (reducedMotion) {
        themeMix = themeTarget
        applyThemeMix(themeMix)
        renderStaticFrame()
      }
    })
    themeObserver.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ['class'],
    })

    const resizeObserver = new ResizeObserver(() => {
      const w = container.clientWidth
      const h = container.clientHeight
      if (w <= 0 || h <= 0) return
      renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 1.5))
      fitMeshToViewport(w, h)
      renderer.setSize(w, h)
      if (reducedMotion) {
        renderStaticFrame()
      }
    })
    resizeObserver.observe(container)

    let rafId = 0
    // Start at a lush pre-warmed phase offset so first frame has full folds
    let elapsed = 14.5
    let lastTs = performance.now()

    const renderFrame = (ts: number) => {
      if (contextLost) return

      const dt = Math.min((ts - lastTs) / 1000, 0.05)
      lastTs = ts
      elapsed += dt

      if (Math.abs(themeTarget - themeMix) > 0.001) {
        themeMix += (themeTarget - themeMix) * Math.min(1, dt * 6)
        applyThemeMix(themeMix)
      }

      const damp = Math.min(1, dt * 3.2)
      currentX += (targetX - currentX) * damp
      currentY += (targetY - currentY) * damp
      pointerActiveCurrent +=
        (pointerActiveTarget - pointerActiveCurrent) * Math.min(1, dt * 2.5)

      material.uniforms.uTime.value = elapsed
      material.uniforms.uPointer.value.set(currentX, currentY)
      material.uniforms.uPointerActive.value = pointerActiveCurrent

      // Subtle tilt follows the pointer like looking across liquid silk
      mesh.rotation.y = currentX * 0.045
      mesh.rotation.x = -currentY * 0.035

      renderer.render(scene, camera)

      if (!reducedMotion && !document.hidden) {
        rafId = window.requestAnimationFrame(renderFrame)
      }
    }

    const startLoop = () => {
      if (contextLost) return
      window.cancelAnimationFrame(rafId)
      lastTs = performance.now()
      rafId = window.requestAnimationFrame(renderFrame)
    }

    const handleVisibility = () => {
      if (document.hidden) {
        window.cancelAnimationFrame(rafId)
      } else if (reducedMotion) {
        renderStaticFrame()
      } else {
        startLoop()
      }
    }

    const handleMotionChange = (e: MediaQueryListEvent) => {
      reducedMotion = e.matches
      if (reducedMotion) {
        window.cancelAnimationFrame(rafId)
        themeMix = themeTarget
        applyThemeMix(themeMix)
        renderStaticFrame()
      } else if (!document.hidden) {
        startLoop()
      }
    }

    const handleContextLost = (e: Event) => {
      e.preventDefault()
      contextLost = true
      window.cancelAnimationFrame(rafId)
    }

    const handleContextRestored = () => {
      contextLost = false
      themeMix = themeTarget
      applyThemeMix(themeMix)
      if (reducedMotion) {
        renderStaticFrame()
      } else if (!document.hidden) {
        startLoop()
      }
    }

    renderer.domElement.addEventListener('webglcontextlost', handleContextLost)
    renderer.domElement.addEventListener(
      'webglcontextrestored',
      handleContextRestored
    )
    document.addEventListener('visibilitychange', handleVisibility)
    motionQuery?.addEventListener('change', handleMotionChange)

    startLoop()

    return () => {
      window.cancelAnimationFrame(rafId)
      renderer.domElement.removeEventListener(
        'webglcontextlost',
        handleContextLost
      )
      renderer.domElement.removeEventListener(
        'webglcontextrestored',
        handleContextRestored
      )
      document.removeEventListener('visibilitychange', handleVisibility)
      motionQuery?.removeEventListener('change', handleMotionChange)
      window.removeEventListener('pointermove', handlePointerMove)
      document.removeEventListener('pointerleave', handlePointerLeave)
      window.removeEventListener('blur', handlePointerLeave)
      themeObserver.disconnect()
      resizeObserver.disconnect()
      geometry.dispose()
      material.dispose()
      if (renderer.domElement.parentNode === container) {
        container.removeChild(renderer.domElement)
      }
      // dispose() alone leaves the WebGL context alive until the detached canvas
      // is garbage-collected; auth screens mount/unmount on every route switch,
      // so drop the context explicitly instead of waiting on the GC.
      renderer.forceContextLoss()
      renderer.dispose()
    }
  }, [])

  return (
    <div
      ref={containerRef}
      aria-hidden='true'
      className='pointer-events-none absolute inset-0 h-full w-full'
    />
  )
}
