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
	Fallbacks int // questions answered by dead reckoning instead
	Errors    int // model failures (also answered by dead reckoning)
}

// LoadGRU loads the ONNX Runtime library and the model file.
func LoadGRU(modelPath, libPath string) (*GRU, error) {
	ort.SetSharedLibraryPath(libPath)
	if !ort.IsInitialized() {
		if err := ort.InitializeEnvironment(); err != nil {
			return nil, fmt.Errorf("start onnxruntime (%s): %w", libPath, err)
		}
	}
	s, err := ort.NewDynamicAdvancedSession(modelPath, []string{"inputs"}, []string{"pred"}, nil)
	if err != nil {
		return nil, fmt.Errorf("load model %s: %w", modelPath, err)
	}
	return &GRU{session: s}, nil
}

func (g *GRU) Close() {
	g.session.Destroy()
	ort.DestroyEnvironment()
}

func (g *GRU) Name() string { return "gru" }

func (g *GRU) Predict(history []track.Point, horizonSec int64) (float64, float64) {
	g.mu.Lock()
	defer g.mu.Unlock()

	in, ok := features.Inputs(history, horizonSec) // same recipe card as training
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
