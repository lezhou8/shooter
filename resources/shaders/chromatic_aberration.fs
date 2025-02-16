#version 330

in vec2 fragTexCoord;
out vec4 fragColor;

uniform sampler2D texture0;

void main()
{
    // Calculate aberration strength based on distance from center
    vec2 center = vec2(0.5, 0.5);
    float dist = length(fragTexCoord - center); 

    // Scale aberration based on distance (stronger at edges)
    float offset = 0.007 * dist;

    // Apply RGB shifts
    float r = texture(texture0, fragTexCoord + vec2(-offset, 0.0)).r;
    float g = texture(texture0, fragTexCoord).g;
    float b = texture(texture0, fragTexCoord + vec2(offset, 0.0)).b;

    fragColor = vec4(r, g, b, 1.0);
}
