package main

import rl "github.com/gen2brain/raylib-go/raylib"

const (
	internalWindowWidth  = 426
	internalWindowHeight = 240
)

type resources struct {
	textures
	fonts
	sound
	shaders
}

type textures struct {
	renderTexture    rl.RenderTexture2D
	floorTexture     rl.Texture2D
	outerWallTexture rl.Texture2D
	innerWallTexture rl.Texture2D

	handgunShoot rl.Texture2D
	sniperShoot  rl.Texture2D
	sniperScope  rl.Texture2D

	otherPlayerA      rl.Texture2D
	otherPlayerB      rl.Texture2D
	deadPlayerTexture rl.Texture2D
}

type fonts struct {
	mainFont rl.Font
}

type sound struct {
	handgunShootSound  rl.Sound
	handgunReloadSound rl.Sound
	sniperShootSound   rl.Sound
	sniperReloadSound  rl.Sound
	genericShootSound  rl.Sound
	swapSound          rl.Sound
	hitMarkerSound     rl.Sound
}

type shaders struct {
	chromaticAberration rl.Shader
}

func (resources *resources) loadResources() {
	resources.renderTexture = rl.LoadRenderTexture(internalWindowWidth, internalWindowHeight)
	resources.floorTexture = rl.LoadTexture("resources/textures/floor_texture.png")
	resources.outerWallTexture = rl.LoadTexture("resources/textures/outer_wall_texture.png")
	resources.innerWallTexture = rl.LoadTexture("resources/textures/inner_wall_texture.png")
	resources.handgunShoot = rl.LoadTexture("resources/textures/handgun_shoot.png")
	resources.sniperShoot = rl.LoadTexture("resources/textures/sniper_shoot.png")
	resources.sniperScope = rl.LoadTexture("resources/textures/sniper_scope.png")
	resources.otherPlayerA = rl.LoadTexture("resources/textures/other_player_a.png")
	resources.otherPlayerB = rl.LoadTexture("resources/textures/other_player_b.png")
	resources.deadPlayerTexture = rl.LoadTexture("resources/textures/dead.png")

	resources.mainFont = rl.LoadFont("resources/fonts/FSEX300.ttf")

	rl.InitAudioDevice()
	resources.handgunShootSound = rl.LoadSound("resources/sounds/handgun_shoot.wav")
	resources.handgunReloadSound = rl.LoadSound("resources/sounds/handgun_reload.wav")
	resources.sniperShootSound = rl.LoadSound("resources/sounds/sniper_shoot.wav")
	resources.sniperReloadSound = rl.LoadSound("resources/sounds/sniper_reload.wav")
	resources.genericShootSound = rl.LoadSound("resources/sounds/generic_gunshot.wav")
	resources.swapSound = rl.LoadSound("resources/sounds/swap_sound.wav")
	resources.hitMarkerSound = rl.LoadSound("resources/sounds/hit_marker.wav")
	rl.SetSoundVolume(resources.hitMarkerSound, 5)

	resources.chromaticAberration = rl.LoadShader("", "resources/shaders/chromatic_aberration.fs")
}

func (resources *resources) unloadResources() {
	rl.UnloadRenderTexture(resources.renderTexture)
	rl.UnloadTexture(resources.floorTexture)
	rl.UnloadTexture(resources.outerWallTexture)
	rl.UnloadTexture(resources.innerWallTexture)
	rl.UnloadTexture(resources.handgunShoot)
	rl.UnloadTexture(resources.sniperShoot)
	rl.UnloadTexture(resources.sniperScope)
	rl.UnloadTexture(resources.otherPlayerA)
	rl.UnloadTexture(resources.otherPlayerB)
	rl.UnloadTexture(resources.deadPlayerTexture)

	rl.UnloadFont(resources.mainFont)

	rl.CloseAudioDevice()
	rl.UnloadSound(resources.handgunShootSound)
	rl.UnloadSound(resources.handgunReloadSound)
	rl.UnloadSound(resources.sniperShootSound)
	rl.UnloadSound(resources.sniperReloadSound)
	rl.UnloadSound(resources.genericShootSound)
	rl.UnloadSound(resources.swapSound)
	rl.UnloadSound(resources.hitMarkerSound)

	rl.UnloadShader(resources.chromaticAberration)
}
