# Shooter

Multiplayer first-person-shooter

## Build

### Server

```{sh}
make server
```

### Client

```{sh}
make client
```

## Running

### Server

```{sh}
./build/server [port] [num-players]
```

- Choose the number of players for the game
- Maximum of 6 players

### Client

```{sh}
./build/client [IP] [port] [ID]
```

- ID's range from 0 to 5
- ID's 0 to 2 are in team A
- ID's 3 to 5 are in team B

## Play

### Controls

- WASD for movement
- Space to jump
- Mouse for looking
- Shift for slow movement
- Left click to shoot
- Right click to use scope
- R to reload
- Q to swap guns

### Rules

- 10 rounds
- The team with the last player(s) standing wins a point

## Acknowledgements

- [Wall and floor textures](https://screamingbrainstudios.itch.io/tiny-texture-pack-2)
- [Wall texture](https://thatguynm.itch.io/lo-fi-textures)
- [Gun sounds](https://f8studios.itch.io/snakes-authentic-gun-sounds)
- [Hit marker sounds](https://filmcow.itch.io/filmcow-sfx)
- [Font](https://github.com/foxoman/fixedsys)
