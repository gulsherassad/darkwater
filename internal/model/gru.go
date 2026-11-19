package model

import (
	"fmt"
	"log"
	"sync"

	ort "github.com/yalue/onnxruntime_go"

	"github.com/gulsherassad/darkwater/internal/features"
	"github.com/gulsherassad/darkwater/internal/predict"
	"github.com/gulsherassad/darkwater/internal/track"
)

// GRU runs the trained ONNX trajectory model.
// It falls back to dead reckoning when the history isn't usable.
type GRU struct {
	mu        sync.Mutex // ONNX session: one prediction at a time
	session   *ort.DynamicAdvancedSession
	Spec      features.Spec // input recipe, picked from the model's input width
	Fallbacks int           // questions answered by dead reckoning instead
	Errors    int           // model failures (also answered by dead reckoning)
}

// The ONNX Runtime environment is shared by every loaded model
var (
	envMu   sync.Mutex
	envRefs int
)

// LoadGRU loads the ONNX Runtime library and the model file. The feature
// recipe (dense or sparse) is chosen from the model's input width, so a model
// can never be fed features it wasn't trained on.
func LoadGRU(modelPath, libPath string) (*GRU, error) {
	envMu.Lock()
	defer envMu.Unlock()
	if !ort.IsInitialized() {
		ort.SetSharedLibraryPath(libPath)
		if err := ort.InitializeEnvironment(); err != nil {
			return nil, fmt.Errorf("start onnxruntime (%s): %w", libPath, err)
		}
	}
	envRefs++
	g, err := load(modelPath)
	if err != nil {
		release()
		return nil, err
	}
	return g, nil
}

func load(modelPath string) (*GRU, error) {
	ins, _, err := ort.GetInputOutputInfo(modelPath)
	if err != nil {
		return nil, fmt.Errorf("read model %s: %w", modelPath, err)
	}
	if len(ins) != 1 || len(ins[0].Dimensions) != 2 {
		return nil, fmt.Errorf("model %s: want one [batch, n] input", modelPath)
	}
	spec, err := features.SpecForInputs(int(ins[0].Dimensions[1]))
	if err != nil {
		return nil, fmt.Errorf("model %s: %w", modelPath, err)
	}
	s, err := ort.NewDynamicAdvancedSession(modelPath, []string{"inputs"}, []string{"pred"}, nil)
	if err != nil {
		return nil, fmt.Errorf("load model %s: %w", modelPath, err)
	}
	return &GRU{session: s, Spec: spec}, nil
}

// release drops one reference to the environment; call with envMu held.
func release() {
	envRefs--
	if envRefs == 0 {
		ort.DestroyEnvironment()
	}
}

func (g *GRU) Close() {
	g.session.Destroy()
	envMu.Lock()
	defer envMu.Unlock()
	release()
}

// Name is "gru" for the dense model (as in earlier results) and "gru-sparse" for the sparse one.
func (g *GRU) Name() string {
	if g.Spec.Version == features.Dense.Version {
		return "gru"
	}
	return "gru-sparse"
}

// Usable reports whether the model can read this history (else it falls back
// to dead reckoning). Lets predict.Hybrid choose between models.
func (g *GRU) Usable(history []track.Point) bool { return g.Spec.Usable(history) }

func (g *GRU) Predict(history []track.Point, horizonSec int64) (float64, float64) {
	g.mu.Lock()
	defer g.mu.Unlock()

	in, ok := g.Spec.Inputs(history, horizonSec) // same recipe card as training
	if !ok {
		g.Fallbacks++
		return predict.DeadReckoning{}.Predict(history, horizonSec)
	}

	dx, dy, err := g.run(in)
	if err != nil {
		if g.Errors == 0 {
			log.Println("gru: ", err) // log the first failure only
		}
		g.Errors++
		return predict.DeadReckoning{}.Predict(history, horizonSec)
	}

	last := history[len(history)-1]
	return features.FromLocalKm(last.Lat, last.Lon, dx, dy)
}

// run feeds one example through the model and returns the predicted (dx, dy) in km.
func (g *GRU) run(in []float32) (float64, float64, error) {
	input, err := ort.NewTensor(ort.NewShape(1, int64(len(in))), in)
	if err != nil {
		return 0, 0, err
	}
	defer input.Destroy()

	output, err := ort.NewEmptyTensor[float32](ort.NewShape(1, 2))
	if err != nil {
		return 0, 0, err
	}
	defer output.Destroy()

	if err := g.session.Run([]ort.Value{input}, []ort.Value{output}); err != nil {
		return 0, 0, err
	}
	out := output.GetData()
	return float64(out[0]), float64(out[1]), nil
}
