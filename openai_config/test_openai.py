import os
from openai import OpenAI
from dotenv import load_dotenv

# Load environment variables from .env file
load_dotenv()

# Initialize OpenAI client with custom configuration
client = OpenAI(
    base_url=os.getenv("OPENAI_BASE_URL", "https://api2api.709970.xyz/v1"),
    api_key=os.getenv("OPENAI_API_KEY"),
)

def test_connection():
    """Test the OpenAI API connection"""
    try:
        response = client.chat.completions.create(
            model=os.getenv("OPENAI_MODEL", "glm-5-turbo"),
            messages=[{"role": "user", "content": "你好！"}],
        )
        print("Success! Response:")
        print(response.choices[0].message.content)
        return True
    except Exception as e:
        print(f"Error: {type(e).__name__}: {e}")
        return False

if __name__ == "__main__":
    print("Testing OpenAI configuration...")
    print(f"Base URL: {os.getenv('OPENAI_BASE_URL')}")
    print(f"Model: {os.getenv('OPENAI_MODEL', 'glm-5-turbo')}")
    print(f"API Key set: {'Yes' if os.getenv('OPENAI_API_KEY') else 'No'}")
    print()
    
    test_connection()