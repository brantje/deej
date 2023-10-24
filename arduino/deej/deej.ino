/*********
# Copyright (C) Sander Brand
# This file is part of Deej <https://github.com/brantje/deej>.
#
# deej is free software: you can redistribute it and/or modify
# it under the terms of the GNU General Public License as published by
# the Free Software Foundation, either version 3 of the License, or
# (at your option) any later version.
#
# deej is distributed in the hope that it will be useful,
# but WITHOUT ANY WARRANTY; without even the implied warranty of
# MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
# GNU General Public License for more details.
#
# You should have received a copy of the GNU General Public License
# along with deej.  If not, see <http://www.gnu.org/licenses/>.
*********/
#include <Wire.h>
#include <Adafruit_GFX.h>
#include <Adafruit_SSD1306.h>
#include <math.h> // For fabs()

#define SCREEN_WIDTH 128
#define SCREEN_HEIGHT 64
#define TCA9548A_ADDRESS 0x70
#define LCD_I2C_ADDRESS 0x3C

#define NOISE_REDUCTION "low"
#define NOISE_REDUCTION_HIGH "high"
#define NOISE_REDUCTION_LOW "low"

const int NUM_SLIDERS = 5;
const int NUM_DISPLAYS = NUM_SLIDERS; // Change if your display amount is different
const int ANALOG_INPUTS[NUM_SLIDERS] = {A0, A1, A2, A6, A3};
const int DISPLAY_SLIDER_MAP[NUM_DISPLAYS] = {0, 1, 4, 3, 2}; // Slider -> Display
const bool INVERT_SLIDERS = false;
int LOOP_COUNTER = 0;
int ANALOG_SLIDER_VALUES[NUM_SLIDERS];
float DISPLAY_VALUES[NUM_DISPLAYS];

const char START_SERIAL_TAG[] = "<<START>>";
const char END_SERIAL_TAG[] = "<<END>>";
boolean isReceiving = false;
int display_idx = 1;
int x = 0, y = 0;

const int BUFFER_SIZE = 128;
char buffer[BUFFER_SIZE];
int bufferIndex = 0;
enum State
{
  WAITING_FOR_START,
  RECEIVING_DATA,
  WAITING_FOR_END
};
State currentState = WAITING_FOR_START;

bool INITIALIZED = false;

Adafruit_SSD1306 display(SCREEN_WIDTH, SCREEN_HEIGHT, &Wire, -1);

void setup()
{

  for (int i = 0; i < NUM_SLIDERS; i++)
  {
    pinMode(ANALOG_INPUTS[i], INPUT);
    ANALOG_SLIDER_VALUES[i] = getSliderValue(ANALOG_INPUTS[i]);
  }
  Wire.begin();
  for (int i = 0; i < NUM_DISPLAYS; i++)
  {
    DISPLAY_VALUES[i] = 100;
    initializeDisplay(DISPLAY_SLIDER_MAP[i]);
  }
  Serial.begin(9600);
  INITIALIZED = true;
  delay(1000);
}

void loop()
{
  updateSliderValues();
  sendSliderValues(); // Actually send data (all the time)
  recvWithStartEndMarkers();
  // if (INITIALIZED)
  // {
  //   for (int i = 0; i < NUM_DISPLAYS; i++)
  //   {
  //     unsigned long now = millis();
  //     long diff = now - DISPLAY_LAST_CHANGED_TIME[i];
  //     if (diff > 2000 && DISPLAY_LAST_CHANGED_TIME[i] != -1)
  //     {
  //       // showIcon(i, i);
  //     }
  //   }
  // }
  // printSliderValues(); // For debug
  delay(10);
}

void TCA9548A(uint8_t bus)
{
  Wire.beginTransmission(TCA9548A_ADDRESS); // TCA9548A address
  Wire.write(1 << bus);                     // send byte to select bus
  Wire.endTransmission();
}

void initializeDisplay(int display_idx)
{
  TCA9548A(display_idx);
  if (!display.begin(SSD1306_SWITCHCAPVCC, LCD_I2C_ADDRESS))
  {
    Serial.println(F("SSD1306 allocation failed"));
    for (;;)
      ;
  }
  // Clear the buffer
  display.clearDisplay();
}

void recvWithStartEndMarkers()
{
  while (Serial.available())
  {
    char c = Serial.read();
    if (bufferIndex < BUFFER_SIZE - 1)
    {
      buffer[bufferIndex] = c;
      bufferIndex++;
    }
    else
    {
      // Reset buffer if overflowed
      bufferIndex = 0;
      memset(buffer, 0, BUFFER_SIZE);
    }

    switch (currentState)
    {
    case WAITING_FOR_START:

      if (bufferIndex >= 9 && strncmp(buffer + bufferIndex - strlen(START_SERIAL_TAG), START_SERIAL_TAG, strlen(START_SERIAL_TAG)) == 0)
      {
        // showText(0, "Receiving", 2);
        int display_idx = Serial.readStringUntil('|').toInt();
        currentState = RECEIVING_DATA;
        memset(buffer, 0, BUFFER_SIZE); // Clear buffer
        bufferIndex = 0;
        TCA9548A(DISPLAY_SLIDER_MAP[display_idx]);
        // display.clearDisplay();
      }
      break;

    case RECEIVING_DATA:
      if (bufferIndex >= 7 && strncmp(buffer + bufferIndex - strlen(END_SERIAL_TAG), END_SERIAL_TAG, strlen(END_SERIAL_TAG)) == 0)
      {
        display.display();
        currentState = WAITING_FOR_START;
        isReceiving = false;
        memset(buffer, 0, BUFFER_SIZE); // Clear buffer
        bufferIndex = 0;
        // showText(0, "Ok", 2);
        // Serial.println("Image drawn on display");
        x = 0;
        y = 0;
      }
      else
      {
        for (int bit = 7; bit >= 0; bit--)
        {
          int pixelValue = (c & (1 << bit)) ? 1 : 0;
          display.drawPixel(x, y, pixelValue);
          x++;
          if (x >= SCREEN_WIDTH)
          {
            x = 0;
            y++;
          }
        }
      }
      break;
    }
  }
}

void showVolume(int display_idx, int volume)
{
  return;
  TCA9548A(display_idx);
  int16_t x1;
  int16_t y1;
  uint16_t width;
  uint16_t height;

  String vol = String(volume);

  int posTop = 2;

  display.setTextSize(2);
  display.getTextBounds(vol, 0, 0, &x1, &y1, &width, &height);
  int posRight = SCREEN_WIDTH - width;

  display.fillRect(posRight, posTop, width, height, BLACK);

  display.setTextColor(WHITE);
  display.setCursor((SCREEN_WIDTH - width), posTop);
  display.println(vol); // text to display
  display.display();
}

int getSliderValue(int input)
{
  int value = analogRead(input);
  if (INVERT_SLIDERS)
  {
    value = map(value, 0, 1023, 1023, 0);
  }
  return value;
}

void updateSliderValues()
{
  for (int i = 0; i < NUM_SLIDERS; i++)
  {
    int value = getSliderValue(ANALOG_INPUTS[i]);
    if (INITIALIZED)
    {
      int normalizedOldValue = ANALOG_SLIDER_VALUES[i];
      float normalizedNewValue = map(value, 0, 1020, 0, 100);
      if (significantlyDifferent(DISPLAY_VALUES[i], normalizedNewValue, NOISE_REDUCTION))
      {
        showVolume(DISPLAY_SLIDER_MAP[i], normalizedNewValue);
        DISPLAY_VALUES[i] = normalizedNewValue;
      }
    }
    ANALOG_SLIDER_VALUES[i] = value;
  }
}

bool almostEquals(float a, float b)
{
  return fabs(a - b) < 0.5; // Using a small threshold to determine equality
}

bool significantlyDifferent(float oldVal, float newVal, const char *noiseReductionLevel)
{
  float significantDifferenceThreshold;

  if (strcmp(noiseReductionLevel, NOISE_REDUCTION_HIGH) == 0)
  {
    significantDifferenceThreshold = 3.5;
  }
  else if (strcmp(noiseReductionLevel, NOISE_REDUCTION_LOW) == 0)
  {
    significantDifferenceThreshold = 1.5;
  }
  else
  {
    significantDifferenceThreshold = 2.5;
  }

  if (fabs(oldVal - newVal) >= significantDifferenceThreshold)
  {
    return true;
  }

  // Special behavior is needed around the edges of 0.0 and 1.0
  if ((almostEquals(newVal, 100) && oldVal != 100) || (almostEquals(newVal, 0.0) && oldVal != 0))
  {
    return true;
  }

  // Values are close enough to not warrant any action
  return false;
}

void sendSliderValues()
{
  String builtString = String("");

  for (int i = 0; i < NUM_SLIDERS; i++)
  {
    builtString += String((int)ANALOG_SLIDER_VALUES[i]);

    if (i < NUM_SLIDERS - 1)
    {
      builtString += String("|");
    }
  }

  Serial.println(builtString);
}

void printSliderValues()
{
  for (int i = 0; i < NUM_SLIDERS; i++)
  {
    String printedString = String("Slider #") + String(i + 1) + String(": ") + String(ANALOG_SLIDER_VALUES[i]) + String(" mV");
    Serial.write(printedString.c_str());

    if (i < NUM_SLIDERS - 1)
    {
      Serial.write(" | ");
    }
    else
    {
      Serial.write("\n");
    }
  }
}
