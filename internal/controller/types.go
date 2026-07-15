package controller

type RunOptions struct {
	ProjectRoot         string
	OutputDir           string
	Image               string
	NoBuild             bool
	Keep                bool
	ReadyTimeoutSeconds int
}

type RunMetadata struct {
	RunID          string         `yaml:"run_id" json:"run_id"`
	DeploymentMode string         `yaml:"deployment_mode" json:"deployment_mode"`
	ProjectName    string         `yaml:"project_name" json:"project_name"`
	ComposeFile    string         `yaml:"compose_file,omitempty" json:"compose_file,omitempty"`
	StackFile      string         `yaml:"stack_file,omitempty" json:"stack_file,omitempty"`
	ScenarioFile   string         `yaml:"scenario_file" json:"scenario_file"`
	ControlPorts   map[string]int `yaml:"control_ports,omitempty" json:"control_ports,omitempty"`
	ControllerURL  string         `yaml:"controller_url,omitempty" json:"controller_url,omitempty"`
}
