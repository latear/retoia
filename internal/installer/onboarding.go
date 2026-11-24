package installer

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	onboardingStart = "<!-- retoia:onboarding:start -->"
	onboardingEnd   = "<!-- retoia:onboarding:end -->"
)

// retoTemplate is the user-editable challenge rules doc; written only if absent.
const retoTemplate = `# Reglas del Reto

> Editá este archivo libremente. Define el reto que el agente debe respetar
> cuando te hace el onboarding. El agente lo lee antes de preguntarte qué querés
> construir. Dejá las secciones que uses y borrá las que no.

## Objetivo del reto
<!-- Qué se busca construir o resolver -->

## Restricciones
<!-- Tecnologías permitidas o prohibidas, límites de tiempo, alcance -->

## Entregables
<!-- Qué tiene que entregar el participante -->

## Criterios de evaluación
<!-- Cómo se evalúa el resultado -->
`

// PrincipalAgentPrompt is the inline system prompt of the principal agent. It is
// the reliable lever that makes onboarding run on the first interaction; the
// AGENTS.md block is only the backstop.
const PrincipalAgentPrompt = `Sos el agente principal de Reto IA. Apenas empieza una sesión en un proyecto —incluso si el usuario solo dice "hola"—, tu PRIMERA respuesta es el onboarding del reto, sin esperar a que te lo pidan:
1. Saludá en una línea y, si existe ./RETO.md, leélo y resumí las reglas del reto en una o dos líneas.
2. Preguntale al usuario qué quiere construir para cumplir el reto, UNA pregunta enfocada por vez (objetivo, alcance, restricciones, stack), y frená a esperar cada respuesta.
3. Cuando el objetivo y el alcance estén claros, escribí ENGAGEMENT.md en la raíz del proyecto con: Objetivo, Alcance, Restricciones, Entregables y Preguntas abiertas. Recién ahí empezá a construir.
Si ya existe ENGAGEMENT.md, no repitas el onboarding: retomá desde ahí. Mantené ENGAGEMENT.md actualizado a medida que avanza el trabajo. Respondé siempre en el idioma del usuario.`

// onboardingBlock is the retoia-managed AGENTS.md block that makes the
// principal agent onboard the user against RETO.md.
const onboardingBlock = onboardingStart + `
## Reto IA — onboarding

Tu PRIMERA respuesta en una sesión nueva DEBE ser este onboarding (aunque el usuario solo diga «hola»).

Sos el agente principal de Reto IA. Las reglas del reto están en ` + "`RETO.md`" + ` (en la
raíz del proyecto o junto a este archivo); el usuario las edita libremente.

En la primera interacción dentro de un proyecto, si todavía no existe ` + "`ENGAGEMENT.md`" + `:
1. Leé ` + "`RETO.md`" + ` y resumí las reglas del reto en una o dos líneas.
2. Entrevistá al usuario sobre qué quiere construir, siempre dentro de esas reglas:
   una pregunta enfocada por vez (objetivo, alcance, restricciones, stack), y frená
   a esperar cada respuesta.
3. Cuando el objetivo y el alcance estén claros, escribí ` + "`ENGAGEMENT.md`" + ` en la raíz
   del proyecto con: Objetivo, Alcance, Restricciones, Entregables y Preguntas abiertas.
Mantené ` + "`ENGAGEMENT.md`" + ` actualizado a medida que el trabajo evoluciona. Respondé en
el idioma del usuario.
` + onboardingEnd + "\n"

// WriteOnboardingDocs places the onboarding docs in configDir: RETO.md only when
// absent (the user owns it), and the managed block in AGENTS.md (created, or
// merged without touching anything outside the markers).
func WriteOnboardingDocs(configDir string) (wroteReto, refreshedAgents bool, err error) {
	if err = os.MkdirAll(configDir, 0o755); err != nil {
		return false, false, err
	}
	retoPath := filepath.Join(configDir, "RETO.md")
	if _, serr := os.Stat(retoPath); os.IsNotExist(serr) {
		if err = os.WriteFile(retoPath, []byte(retoTemplate), 0o644); err != nil {
			return false, false, err
		}
		wroteReto = true
	} else if serr != nil {
		return false, false, serr
	}

	agentsPath := filepath.Join(configDir, "AGENTS.md")
	existing, rerr := os.ReadFile(agentsPath)
	if rerr != nil && !os.IsNotExist(rerr) {
		return wroteReto, false, rerr
	}
	merged := mergeOnboardingBlock(string(existing))
	if merged == string(existing) {
		return wroteReto, false, nil
	}
	if err = os.WriteFile(agentsPath, []byte(merged), 0o644); err != nil {
		return wroteReto, false, err
	}
	return wroteReto, true, nil
}

// mergeOnboardingBlock replaces the managed block when both markers exist, else
// appends it.
func mergeOnboardingBlock(content string) string {
	i := strings.Index(content, onboardingStart)
	if i >= 0 {
		if j := strings.Index(content[i:], onboardingEnd); j >= 0 {
			end := i + j + len(onboardingEnd)
			end += len(content[end:]) - len(strings.TrimPrefix(content[end:], "\n"))
			return content[:i] + onboardingBlock + content[end:]
		}
	}
	if content == "" {
		return onboardingBlock
	}
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return content + "\n" + onboardingBlock
}
